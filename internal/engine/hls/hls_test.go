package hls

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
