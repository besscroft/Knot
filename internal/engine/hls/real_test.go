//go:build hlsnet

// 真实流端到端验证（依赖公网，默认不跑）：
//
//	go test -tags hlsnet -run TestRealStream -v ./internal/engine/hls/
package hls

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealStreamEndToEnd 用 Apple 的公开测试流（bipbop，VOD）验证
// 分片下载 → 拼接 → 转封装 MP4 的完整链路。
func TestRealStreamEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过网络测试")
	}
	// gear1 为最低码率变体，约 180 片 × 300KB。
	const mediaURL = "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_4x3/gear1/prog_index.m3u8"

	dir := t.TempDir()
	finalPath := filepath.Join(dir, "bipbop.mp4")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err := Download(ctx, mediaURL, filepath.Join(dir, "work"), finalPath, Options{Threads: 8})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 1<<20 {
		t.Fatalf("MP4 过小: %d bytes", info.Size())
	}
	head := make([]byte, 12)
	f, err := os.Open(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatal(err)
	}
	// MP4 文件以 4 字节长度 + "ftyp" 开始。
	if string(head[4:8]) != "ftyp" {
		t.Fatalf("不是有效的 MP4 文件头: %x", head)
	}
	t.Logf("OK: %s = %.1f MB", finalPath, float64(info.Size())/(1<<20))
}
