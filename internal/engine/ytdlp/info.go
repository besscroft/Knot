// yt-dlp 解析：-J 输出的 JSON 拆解为标题与画质档位。画质按短边像素分档
// （横屏即高度，竖屏短片即宽度，用户感知一致），同档多编码取兼容性最好的；
// 大小按各路格式的 filesize（缺失时用码率×时长估算）相加。
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Info 是 yt-dlp -J 的解析结果（服务层映射为前端的画质列表）。
type Info struct {
	ID        string
	Title     string
	Duration  float64 // 秒，0 = 未知
	Live      bool
	Qualities []Quality // 按档位降序
}

// Quality 是一个可选画质档位。ID 为档位值（短边像素），档位即解析到的
// 具体格式 ID（videoID/audioID），拼装下载用的 -f 表达式时优先精确匹配，
// 个别格式下线时仍有兜底链。
type Quality struct {
	ID     int    // 档位值（短边像素），0 = 最佳
	Desc   string // 如 "1080p60"
	Codecs string // 兼容性最好一路的编码（展示用，如 "H.264"）
	Fps    int    // 0 = 无帧率信息
	Size   int64  // 视频+音频估算字节数，0 = 未知
	// 实际采用的 yt-dlp format id；合并格式（音频内嵌）时 audioID 为空，
	// 纯音频档位（音质）不存在，音频只作为 -x 的目标。
	videoID string
	audioID string
}

// jsonInfo 是 yt-dlp -J 输出里用到的字段（其余忽略）。
type jsonInfo struct {
	Type     string       `json:"_type"` // "playlist" = 播放列表页
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Duration float64      `json:"duration"`
	Live     bool         `json:"is_live"`
	Entries  []jsonInfo   `json:"entries"`
	Formats  []jsonFormat `json:"formats"`
}

// jsonFormat 是 -J 输出里的单个格式（一路视频或音频，或合并的渐进式）。
type jsonFormat struct {
	FormatID       string  `json:"format_id"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Fps            float64 `json:"fps"`
	VCodec         string  `json:"vcodec"` // "none" = 纯音频
	ACodec         string  `json:"acodec"` // "none" = 纯视频
	Ext            string  `json:"ext"`
	Filesize       int64   `json:"filesize"`
	FilesizeApprox int64   `json:"filesize_approx"`
	TBR            float64 `json:"tbr"` // 总码率，千字节/秒
	Protocol       string  `json:"protocol"`
}

// Resolve 解析单个视频的可用画质。bin 须已就绪（Ensure），proxy 为空时
// yt-dlp 跟随环境变量。播放列表页与直播流返回明确的中文错误。
func Resolve(ctx context.Context, bin, rawURL, proxy string) (*Info, error) {
	args := []string{"-J", "--no-playlist"}
	if proxy != "" {
		args = append(args, "--proxy", proxy)
	}
	args = append(args, rawURL)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // 页面关闭等取消：保持错误链
		}
		return nil, fmt.Errorf("解析视频信息失败: %s", ytError(stderr.String(), err))
	}
	var raw jsonInfo
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("解析 yt-dlp 输出失败: %w", err)
	}
	if raw.Type == "playlist" {
		return nil, fmt.Errorf("播放列表暂不支持下载，请使用单个视频的链接")
	}
	return infoFrom(&raw), nil
}

// ytError 从 yt-dlp 的 stderr 提取 ERROR 行（可读原因），缺失时退回尾部。
func ytError(stderr string, err error) string {
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		if msg, ok := strings.CutPrefix(line, "ERROR: "); ok && msg != "" {
			return msg
		}
	}
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		return err.Error()
	}
	// 兜底取尾部：ERROR 行解析失败时至少给出最近的原因。
	if len(detail) > 500 {
		detail = detail[len(detail)-500:]
	}
	return detail
}

// infoFrom 把 -J 输出整理为画质档位。
func infoFrom(raw *jsonInfo) *Info {
	info := &Info{ID: raw.ID, Title: raw.Title, Duration: raw.Duration, Live: raw.Live}

	var videos, audios []jsonFormat
	for _, f := range raw.Formats {
		if strings.Contains(f.Protocol, "m3u8") {
			continue // 分片流质量与稳定性都不如直链，画质列表只收直链
		}
		switch {
		case f.VCodec != "" && f.VCodec != "none" && f.Height > 0:
			videos = append(videos, f)
		case f.ACodec != "" && f.ACodec != "none":
			audios = append(audios, f)
		}
	}

	best := pickAudio(audios)
	audioSize := formatSize(best, raw.Duration)

	// 分档：短边像素（竖屏短片取宽度）；同档多编码取兼容性最好的，
	// 并列时取码率更高的。
	pick := make(map[int]jsonFormat, len(videos))
	for _, v := range videos {
		dim := v.Height
		if v.Width > 0 && v.Width < dim {
			dim = v.Width
		}
		if ex, ok := pick[dim]; ok {
			if ytCodecRank(v.VCodec) < ytCodecRank(ex.VCodec) ||
				(ytCodecRank(v.VCodec) == ytCodecRank(ex.VCodec) && v.TBR > ex.TBR) {
				pick[dim] = v
			}
			continue
		}
		pick[dim] = v
	}
	info.Qualities = make([]Quality, 0, len(pick))
	for dim, v := range pick {
		q := Quality{
			ID:      dim,
			Desc:    qualityDesc(dim, v.Fps),
			Codecs:  codecDesc(v.VCodec),
			Fps:     int(v.Fps),
			videoID: v.FormatID,
		}
		if v.ACodec == "none" {
			// 纯视频分轨：音轨单独一路，大小相加；合并格式音轨内嵌不计。
			q.audioID = best.FormatID
			q.Size = formatSize(v, raw.Duration) + audioSize
		} else {
			q.Size = formatSize(v, raw.Duration)
		}
		info.Qualities = append(info.Qualities, q)
	}
	sort.Slice(info.Qualities, func(i, j int) bool {
		return info.Qualities[i].ID > info.Qualities[j].ID
	})
	return info
}

// Quality 返回指定档位；档位不存在时回退最高档，无画质返回 nil。
func (i *Info) Quality(qualityID int) *Quality {
	if len(i.Qualities) == 0 {
		return nil
	}
	for idx, q := range i.Qualities {
		if q.ID == qualityID {
			return &i.Qualities[idx]
		}
	}
	return &i.Qualities[0]
}

// FormatSelector 组装 yt-dlp 的 -f 表达式：优先该档位解析到的具体格式
// （对竖屏短片等 height 语义不匹配的情况也准确），其后按档位高度兜底，
// 最后回退最佳。audioOnly 时只要音轨。
func (i *Info) FormatSelector(qualityID int, audioOnly bool) string {
	q := i.Quality(qualityID)
	if audioOnly {
		if q != nil && q.audioID != "" {
			return q.audioID + "/ba/b"
		}
		return "ba/b"
	}
	if q == nil {
		return "bv*+ba/b"
	}
	primary := q.videoID
	if q.audioID != "" {
		primary += "+" + q.audioID
	}
	fallback := fmt.Sprintf("bv*[height<=%d]+ba/b[height<=%d]", q.ID, q.ID)
	return primary + "/" + fallback
}

// pickAudio 取码率最高的音轨（并列时偏好 m4a，封装兼容性更好）。
func pickAudio(audios []jsonFormat) jsonFormat {
	var best jsonFormat
	for _, a := range audios {
		if best.FormatID == "" || a.TBR > best.TBR ||
			(a.TBR == best.TBR && a.Ext == "m4a" && best.Ext != "m4a") {
			best = a
		}
	}
	return best
}

// formatSize 估算一路格式的字节数：filesize 缺失时用码率×时长兜底。
func formatSize(f jsonFormat, duration float64) int64 {
	if f.FormatID == "" {
		return 0
	}
	if f.Filesize > 0 {
		return f.Filesize
	}
	if f.FilesizeApprox > 0 {
		return f.FilesizeApprox
	}
	if f.TBR > 0 && duration > 0 {
		return int64(f.TBR * 1000 / 8 * duration)
	}
	return 0
}

// qualityDesc 档位描述："1080p"，高帧率（≥50）标注帧率如 "1080p60"。
func qualityDesc(dim int, fps float64) string {
	if fps >= 50 {
		return strconv.Itoa(dim) + "p" + strconv.Itoa(int(fps))
	}
	return strconv.Itoa(dim) + "p"
}

// ytCodecRank 编码兼容性排序：avc < hevc < vp9 < av1（越小越优先，
// 与 B 站 pickBiliVideo 的兼容思路一致；未知排最后）。
func ytCodecRank(vcodec string) int {
	c := strings.ToLower(vcodec)
	switch {
	case strings.HasPrefix(c, "avc"), strings.HasPrefix(c, "h264"):
		return 0
	case strings.HasPrefix(c, "hvc"), strings.HasPrefix(c, "hev"), strings.HasPrefix(c, "h265"):
		return 1
	case strings.HasPrefix(c, "vp9"), strings.HasPrefix(c, "vp09"):
		return 2
	case strings.HasPrefix(c, "av01"):
		return 3
	}
	return 4
}

// codecDesc 编码展示名。
func codecDesc(vcodec string) string {
	c := strings.ToLower(vcodec)
	switch {
	case strings.HasPrefix(c, "avc"), strings.HasPrefix(c, "h264"):
		return "H.264"
	case strings.HasPrefix(c, "hvc"), strings.HasPrefix(c, "hev1"), strings.HasPrefix(c, "h265"):
		return "H.265"
	case strings.HasPrefix(c, "vp9"), strings.HasPrefix(c, "vp09"):
		return "VP9"
	case strings.HasPrefix(c, "av01"):
		return "AV1"
	}
	return vcodec
}
