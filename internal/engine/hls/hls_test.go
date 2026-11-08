package hls

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResolvePicksHighestBandwidth(t *testing.T) {
	srv := hlsServer(t)
	got, err := Resolve(context.Background(), srv.Client(), srv.URL+"/master.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != srv.URL+"/high.m3u8" {
		t.Fatalf("Resolve = %q, want %q", got, srv.URL+"/high.m3u8")
	}
}

func TestParseValidatesAndResolves(t *testing.T) {
	srv := hlsServer(t)
	media, err := Parse(context.Background(), srv.Client(), srv.URL+"/high.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(media.Segments) != 3 {
		t.Fatalf("分片数 = %d, want 3", len(media.Segments))
	}
	if media.Segments[0].URL != srv.URL+"/seg0.ts" {
		t.Fatalf("相对地址未解析: %q", media.Segments[0].URL)
	}
	if media.Segments[1].KeyURI != srv.URL+"/key" {
		t.Fatalf("密钥地址未解析: %q", media.Segments[1].KeyURI)
	}
	if media.Segments[0].KeyURI != "" || media.Segments[2].KeyURI != "" {
		t.Fatal("seg0/seg2 不应加密")
	}
}

func TestDownloadSegmentsConcatenates(t *testing.T) {
	srv := hlsServer(t)
	workDir := t.TempDir()

	var lastSegDone, lastSegTotal atomic.Int64
	if _, err := downloadSegments(context.Background(), srv.URL+"/high.m3u8", workDir, Options{
		Threads: 3,
		OnProgress: func(_ int64, segDone, segTotal int) {
			lastSegDone.Store(int64(segDone))
			lastSegTotal.Store(int64(segTotal))
		},
	}); err != nil {
		t.Fatal(err)
	}

	allTS, err := os.ReadFile(filepath.Join(workDir, "all.ts"))
	if err != nil {
		t.Fatal(err)
	}
	// seg1 已解密：内容应等于三个明文分片按序拼接。
	want := bytes.Join([][]byte{
		[]byte("HIGH-SEG-0-CONTENT_______________"),
		[]byte("HIGH-SEG-1-ENCRYPTED-CONTENT_____"),
		[]byte("HIGH-SEG-2-CONTENT_______________"),
	}, nil)
	if !bytes.Equal(allTS, want) {
		t.Fatalf("拼接内容不一致: got %q", allTS)
	}
	if lastSegTotal.Load() != 3 {
		t.Fatalf("进度回调异常: total=%d", lastSegTotal.Load())
	}
	if lastSegDone.Load() > 3 {
		t.Fatalf("进度回调异常: done=%d", lastSegDone.Load())
	}
}

func TestDownloadSegmentsResume(t *testing.T) {
	srv := hlsServer(t)
	workDir := t.TempDir()

	// 预置前两个分片，模拟上次中断。
	if err := os.WriteFile(filepath.Join(workDir, "seg_000000.ts"), []byte("HIGH-SEG-0-CONTENT_______________"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "seg_000001.ts"), []byte("HIGH-SEG-1-ENCRYPTED-CONTENT_____"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := downloadSegments(context.Background(), srv.URL+"/high.m3u8", workDir, Options{Threads: 2}); err != nil {
		t.Fatal(err)
	}

	allTS, _ := os.ReadFile(filepath.Join(workDir, "all.ts"))
	want := bytes.Join([][]byte{
		[]byte("HIGH-SEG-0-CONTENT_______________"),
		[]byte("HIGH-SEG-1-ENCRYPTED-CONTENT_____"),
		[]byte("HIGH-SEG-2-CONTENT_______________"),
	}, nil)
	if !bytes.Equal(allTS, want) {
		t.Fatalf("续传后拼接内容不一致: %q", allTS)
	}
}

func TestDownloadRemuxFallbackKeepsTS(t *testing.T) {
	srv := hlsServer(t)
	workDir := t.TempDir()
	finalPath := filepath.Join(t.TempDir(), "movie.mp4")

	// 非 TS 内容（纯文本分片）无法转封装 → 应报错并保留 .ts 兜底。
	err := Download(context.Background(), srv.URL+"/high.m3u8", workDir, finalPath, Options{Threads: 2})
	if err == nil {
		t.Fatal("无效 TS 应导致转封装失败")
	}
	wantTS := filepath.Join(filepath.Dir(finalPath), "movie.ts")
	if _, statErr := os.Stat(wantTS); statErr != nil {
		t.Fatalf("应保留 .ts 兜底文件: %v", statErr)
	}
	if _, statErr := os.Stat(finalPath); statErr == nil {
		t.Fatal("转封装失败时不应产生 mp4")
	}
}

func TestIVForExplicitAndDerived(t *testing.T) {
	seg := Segment{KeyIV: "0xA0A1A2A3A4A5A6A7A8A9AAABACADAEAF"}
	iv, err := ivFor(seg)
	if err != nil {
		t.Fatal(err)
	}
	if iv[0] != 0xA0 || iv[15] != 0xAF {
		t.Fatalf("显式 IV 解析错误: %x", iv)
	}

	// 无显式 IV：SeqId=1 → 最后一个字节为 1。
	iv, err = ivFor(Segment{SeqId: 1})
	if err != nil {
		t.Fatal(err)
	}
	if iv[15] != 1 {
		t.Fatalf("SeqId 派生 IV 错误: %x", iv)
	}

	if _, err := ivFor(Segment{KeyIV: "0xZZ"}); err == nil {
		t.Fatal("非法 IV 应报错")
	}
}

func TestParseRejectsLivePlaylist(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:4.0,\nseg0.ts\n"))
	}))
	defer live.Close()

	_, err := Parse(context.Background(), live.Client(), live.URL+"/live.m3u8", nil)
	if err == nil {
		t.Fatal("直播流应被拒绝")
	}
}

// TestParseKeyAppliesToAllFollowingSegments 验证 EXT-X-KEY 按规范作用于其后
// 所有分片（Eyevinn 解析器只挂到紧随其后的单个分片，需自行继承）。
// 真实场景（KEY 声明在播放列表头部、之后不再重复）此前会导致首片之后的
// 分片被当作未加密保存密文。
func TestParseKeyAppliesToAllFollowingSegments(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plain := []byte("ENCRYPTED-SEGMENT-CONTENT____________")
	enc := encryptAES128(plain, key, make([]byte, 16))

	list := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-PLAYLIST-TYPE:VOD\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x" + hexEnc(make([]byte, 16)) + "\n" +
		"#EXTINF:4.0,\nseg0.ts\n#EXTINF:4.0,\nseg1.ts\n#EXT-X-ENDLIST\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/list.m3u8", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(list)) })
	mux.HandleFunc("/key", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(key) })
	mux.HandleFunc("/seg0.ts", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(enc) })
	mux.HandleFunc("/seg1.ts", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(enc) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Parse：两个分片都应带上密钥。
	media, err := Parse(context.Background(), srv.Client(), srv.URL+"/list.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, seg := range media.Segments {
		if seg.KeyURI != srv.URL+"/key" {
			t.Fatalf("seg%d 未继承密钥: KeyURI=%q", i, seg.KeyURI)
		}
	}

	// 下载：解密应作用于每个分片，拼接结果为明文。
	allTS, err := downloadSegments(context.Background(), srv.URL+"/list.m3u8", t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(allTS)
	if err != nil {
		t.Fatal(err)
	}
	if want := bytes.Repeat(plain, 2); !bytes.Equal(got, want) {
		t.Fatalf("解密后拼接内容不一致: got %q", got)
	}
}

// TestParseKeyNoneStopsInheritance 验证 METHOD=NONE 之后不再继承密钥。
func TestParseKeyNoneStopsInheritance(t *testing.T) {
	list := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-ENDLIST\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x" + hexEnc(make([]byte, 16)) + "\n" +
		"#EXTINF:4.0,\nseg0.ts\n" +
		"#EXT-X-KEY:METHOD=NONE\n" +
		"#EXTINF:4.0,\nseg1.ts\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(list))
	}))
	defer srv.Close()

	media, err := Parse(context.Background(), srv.Client(), srv.URL+"/list.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	if media.Segments[0].KeyURI == "" {
		t.Fatal("seg0 应加密")
	}
	if media.Segments[1].KeyURI != "" {
		t.Fatal("NONE 之后 seg1 不应继承密钥")
	}
}

// TestDecryptRejectsWrongKey 验证密钥不匹配时明确报错而非静默产出乱码。
func TestDecryptRejectsWrongKey(t *testing.T) {
	realKey := []byte("0123456789abcdef")
	iv := [16]byte{}
	cipherText := encryptAES128([]byte("SOME-TS-PAYLOAD-NOT-ALIGN________"), realKey, iv[:])

	wrongKey := []byte("fedcba9876543210")
	if _, err := decryptAES128(cipherText, wrongKey, iv); err == nil {
		t.Fatal("错误密钥应报错")
	}
	got, err := decryptAES128(cipherText, realKey, iv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, []byte("SOME-TS-PAYLOAD")) {
		t.Fatalf("正确密钥解密结果不符: %q", got)
	}
}

// TestRemuxRejectsGarbageTS 验证整体损坏的 TS 走“数据损坏”报错而非 panic。
func TestRemuxRejectsGarbageTS(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "bad.ts")
	// 伪同步字开头的垃圾包 + 无同步字垃圾，足以让解析器大面积失败。
	buf := make([]byte, 512*tsPacketSize)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	for i := 0; i < len(buf); i += tsPacketSize {
		buf[i] = tsSyncByte // 同步字对但内容全是垃圾
	}
	if err := os.WriteFile(tsPath, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Remux(tsPath, filepath.Join(dir, "bad.mp4"))
	if err == nil {
		t.Fatal("垃圾 TS 应报错")
	}
	if strings.Contains(err.Error(), "转封装异常") {
		t.Fatalf("不应以 panic 兜底: %v", err)
	}
	if !strings.Contains(err.Error(), "损坏") && !strings.Contains(err.Error(), "没有可识别") {
		t.Fatalf("应给出明确损坏提示: %v", err)
	}
}
