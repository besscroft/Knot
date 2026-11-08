// Package hls 实现 m3u8/HLS 流的下载：播放列表解析 → 分片并发下载
// （含 AES-128 解密）→ TS 拼接 → 转封装 MP4。
package hls

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Eyevinn/hls-m3u8/m3u8"
	"github.com/besscroft/knot/internal/engine/httpdl"
)

const userAgent = "Mozilla/5.0 (KHTML, like Gecko) Knot/0.1"

// Media 是一份解析并校验后的点播 media playlist。
type Media struct {
	URL      string    // media playlist 的绝对地址
	Segments []Segment // 按播放顺序排列的分片
}

// Segment 是一个待下载分片。
type Segment struct {
	Index  int    // 播放顺序号
	URL    string // 绝对地址
	SeqId  uint64 // 播放列表序号，用于默认 IV
	KeyURI string // AES-128 密钥地址，空 = 未加密
	KeyIV  string // 播放列表显式 IV（0x...），空 = 用 SeqId 派生
}

// IsPlaylist 报告 URL/响应是否像 m3u8 播放列表。
func IsPlaylist(rawURL, contentType string) bool {
	if contentType != "" {
		mt := strings.ToLower(contentType)
		if strings.Contains(mt, "mpegurl") {
			return true
		}
	}
	return strings.HasSuffix(strings.ToLower(strings.Split(rawURL, "?")[0]), ".m3u8")
}

// fetchBody 用 client 拉取地址内容（跟随重定向），返回最终 URL 与 body。
func fetchBody(ctx context.Context, client *http.Client, rawURL string, headers *httpdl.Headers) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	headers.Apply(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("获取播放列表失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("获取播放列表失败: 服务器返回 %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", nil, err
	}
	final := resp.Request.URL.String()
	return final, body, nil
}

// Resolve 抓取 rawURL（master 或 media playlist）；若是 master 则选最高带宽的
// 变体，返回 media playlist 的绝对地址。
func Resolve(ctx context.Context, client *http.Client, rawURL string, headers *httpdl.Headers) (string, error) {
	final, body, err := fetchBody(ctx, client, rawURL, headers)
	if err != nil {
		return "", err
	}
	if !bytes.Contains(body, []byte("#EXTM3U")) {
		return "", fmt.Errorf("该地址不是 m3u8 播放列表")
	}
	p, listType, err := m3u8.DecodeFrom(bytes.NewReader(body), true)
	if err != nil {
		return "", fmt.Errorf("解析播放列表失败: %w", err)
	}
	if listType == m3u8.MEDIA {
		return final, nil
	}
	master, ok := p.(*m3u8.MasterPlaylist)
	if !ok || len(master.Variants) == 0 {
		return "", fmt.Errorf("播放列表为空")
	}
	best := master.Variants[0]
	for _, v := range master.Variants[1:] {
		if v.VariantParams.Bandwidth > best.VariantParams.Bandwidth {
			best = v
		}
	}
	return resolveRef(final, best.URI)
}

// Parse 抓取并解析 media playlist，校验支持性，返回顺序分片计划。
func Parse(ctx context.Context, client *http.Client, mediaURL string, headers *httpdl.Headers) (*Media, error) {
	final, body, err := fetchBody(ctx, client, mediaURL, headers)
	if err != nil {
		return nil, err
	}
	p, listType, err := m3u8.DecodeFrom(bytes.NewReader(body), true)
	if err != nil {
		return nil, fmt.Errorf("解析播放列表失败: %w", err)
	}
	if listType != m3u8.MEDIA {
		// Create 阶段已解析过 master，这里仍遇到则取最高带宽再试一次。
		media, err := Resolve(ctx, client, final, headers)
		if err != nil {
			return nil, err
		}
		if media == final {
			return nil, fmt.Errorf("播放列表解析异常")
		}
		return Parse(ctx, client, media, headers)
	}
	pl := p.(*m3u8.MediaPlaylist)
	if !pl.Closed {
		return nil, fmt.Errorf("暂不支持直播流（仅支持点播 VOD）")
	}
	if pl.Map != nil {
		return nil, fmt.Errorf("暂不支持 fMP4 分片（EXT-X-MAP）")
	}
	if len(pl.Segments) == 0 {
		return nil, fmt.Errorf("播放列表没有分片")
	}

	m := &Media{URL: final, Segments: make([]Segment, 0, len(pl.Segments))}
	// EXT-X-KEY 按规范作用于其后所有分片（直至下一个 KEY 标签）。
	// Eyevinn 解析器只把 KEY 挂到紧随其后的单个分片上，这里自行维护
	// “当前密钥”逐分片继承，否则首片之后的分片会被当成未加密落盘密文。
	var curURI, curIV string
	haveKey := false
	for i, seg := range pl.Segments {
		if seg == nil {
			continue // 解析器按容量预分配，尾部可能是 nil 占位
		}
		switch {
		case seg.URI == "":
			return nil, fmt.Errorf("播放列表存在空分片")
		case seg.Map != nil:
			return nil, fmt.Errorf("暂不支持 fMP4 分片（EXT-X-MAP）")
		case seg.Limit != 0 || seg.Offset != 0:
			return nil, fmt.Errorf("暂不支持 EXT-X-BYTERANGE 分片")
		case seg.Gap:
			return nil, fmt.Errorf("播放列表存在缺失分片（EXT-X-GAP）")
		}
		s := Segment{Index: i, SeqId: seg.SeqId}
		if s.URL, err = resolveRef(final, seg.URI); err != nil {
			return nil, err
		}
		if len(seg.Keys) > 0 {
			for _, k := range seg.Keys {
				switch strings.ToLower(k.Method) {
				case "", "none":
					curURI, curIV, haveKey = "", "", false
				case "aes-128":
					if k.URI == "" {
						return nil, fmt.Errorf("AES-128 密钥缺少 URI")
					}
					if curURI, err = resolveRef(final, k.URI); err != nil {
						return nil, err
					}
					curIV, haveKey = k.IV, true
				case "sample-aes":
					return nil, fmt.Errorf("暂不支持 SAMPLE-AES 加密")
				default:
					return nil, fmt.Errorf("不支持的加密方式 %q", k.Method)
				}
			}
		}
		if haveKey {
			s.KeyURI, s.KeyIV = curURI, curIV
		}
		m.Segments = append(m.Segments, s)
	}
	return m, nil
}

// resolveRef 相对引用转绝对地址。
func resolveRef(base, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("空引用")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("无效的分片地址 %q: %w", ref, err)
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(u).String(), nil
}

// ivFor 计算分片的 16 字节 IV：显式 IV 优先，否则用 SeqId 的大端 128 位。
func ivFor(seg Segment) ([16]byte, error) {
	var iv [16]byte
	if seg.KeyIV == "" {
		seq := seg.SeqId
		for i := 15; i >= 8 && seq > 0; i-- {
			iv[i] = byte(seq)
			seq >>= 8
		}
		return iv, nil
	}
	hexStr := strings.TrimPrefix(strings.TrimPrefix(seg.KeyIV, "0x"), "0X")
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return iv, fmt.Errorf("IV 无效: %w", err)
	}
	if len(raw) != 16 {
		return iv, fmt.Errorf("IV 长度应为 16 字节，实际 %d", len(raw))
	}
	copy(iv[:], raw)
	return iv, nil
}

// fetchKey 拉取 AES-128 密钥（16 字节）。
func fetchKey(ctx context.Context, client *http.Client, keyURI string, headers *httpdl.Headers) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, keyURI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	headers.Apply(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取密钥失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取密钥失败: 服务器返回 %s", resp.Status)
	}
	key, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return nil, err
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("密钥长度应为 16 字节，实际 %d", len(key))
	}
	return key, nil
}

// decryptAES128 用 AES-128-CBC 解密分片并按 PKCS7 去填充。
// 填充无效且结果不像 TS（不以同步字开头）时判定为密钥/IV 不匹配，
// 直接报错而不是把乱码当明文落盘（那样会拖到转封装阶段才暴露）。
func decryptAES128(data, key []byte, iv [16]byte) ([]byte, error) {
	if len(data) == 0 || len(data)%16 != 0 {
		return nil, fmt.Errorf("分片长度 %d 不是 16 的倍数，无法解密", len(data))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv[:]).CryptBlocks(out, data)
	// PKCS7：末尾 pad 字节都等于 pad 长度才认为是有效填充。
	if n := len(out); n > 0 {
		pad := int(out[n-1])
		valid := pad >= 1 && pad <= 16 && pad <= n
		for i := n - pad; valid && i < n; i++ {
			if int(out[i]) != pad {
				valid = false
			}
		}
		if valid {
			return out[:n-pad], nil
		}
		// 少数源不按 PKCS7 填充：解密结果以 TS 同步字开头则保留；
		// 否则几乎必然是密钥或 IV 不匹配。
		if out[0] != tsSyncByte {
			return nil, fmt.Errorf("分片解密失败（密钥或 IV 不匹配，可能已失效）")
		}
	}
	return out, nil
}
