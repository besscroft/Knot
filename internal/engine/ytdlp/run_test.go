package ytdlp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestHandleLineProgress(t *testing.T) {
	var p progress
	var got []int64
	on := func(down int64) { got = append(got, down) }

	handleLine(&p, "[download] Destination: 视频标题.f137.mp4", on) // 进入视频阶段
	handleLine(&p, "KNP|100|1000|900", on)
	handleLine(&p, "KNP|100|1000|900", on) // 重复行不累计
	handleLine(&p, "KNP|NA|NA|NA", on)     // 未开始：忽略
	handleLine(&p, "KNP|50|1000|900", on)  // 阶段内回退：单调取大
	handleLine(&p, "[Merger] Merging formats into \"x.mp4\"", on)
	handleLine(&p, "[download] Destination: 视频标题.f140.m4a", on) // 进入音频阶段
	handleLine(&p, "KNP|200|1000|900", on)

	if want := []int64{100, 300}; !slices.Equal(got, want) {
		t.Fatalf("进度回调 = %v, want %v", got, want)
	}
}

func TestHandleLineIgnoresOtherLines(t *testing.T) {
	var p progress
	called := false
	on := func(int64) { called = true }
	for _, line := range []string{
		"[youtube] dQw4w9WgXcQ: Downloading webpage",
		"[download]  45.3% of 10.00MiB at 2.00MiB/s ETA 00:03",
		"KNP",
		"KNP|abc|def",
		"",
	} {
		handleLine(&p, line, on)
	}
	if called {
		t.Fatal("非进度行不应触发回调")
	}
}

func TestDownloadArgsVideo(t *testing.T) {
	args := DownloadArgs(DownloadOptions{
		Dir: `C:\videos`, Base: "50%off 标题", Format: "mp4",
		Selector: "299+251/bv*+ba/b", Ffmpeg: `C:\tools\ffmpeg.exe`, Proxy: "http://127.0.0.1:7890",
	})
	joined := strings.Join(args, "\x00")
	for _, want := range []string{
		"--no-playlist", "--newline",
		"--merge-output-format\x00mp4",
		"--ffmpeg-location\x00C:\\tools\\ffmpeg.exe",
		"--proxy\x00http://127.0.0.1:7890",
		"-f\x00299+251/bv*+ba/b",
		"-o\x0050%%off 标题.%(ext)s", // % 需转义，防止被当模板变量（目录走 -P）
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("参数缺少 %q: %v", want, args)
		}
	}
	if slices.Contains(args, "-x") {
		t.Errorf("视频格式不应带 -x: %v", args)
	}
}

func TestDownloadArgsFormatRouting(t *testing.T) {
	// mov 非原生合流容器：先落 mp4，引擎侧再转换。
	args := DownloadArgs(DownloadOptions{Dir: ".", Base: "a", Format: "mov", Selector: "b"})
	if !slices.Contains(args, "mp4") || slices.Contains(args, "mov") {
		t.Errorf("mov 应回退 mp4 合流: %v", args)
	}
	// 音频格式走 -x --audio-format。
	args = DownloadArgs(DownloadOptions{Dir: ".", Base: "a", Format: "mp3", Selector: "ba/b"})
	joined := strings.Join(args, "\x00")
	if !strings.Contains(joined, "-x\x00--audio-format\x00mp3") {
		t.Errorf("mp3 应走音频提取: %v", args)
	}
	if strings.Contains(joined, "--merge-output-format") {
		t.Errorf("音频格式不应带合流参数: %v", args)
	}
}

func TestDownloadCtxCancel(t *testing.T) {
	// ctx 取消应终止进程并回传 context.Canceled（暂停语义依赖此错误链）。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Download(ctx, "yt-dlp-no-such-bin", DownloadOptions{Dir: t.TempDir(), Base: "x", Selector: "b"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后错误 = %v, want context.Canceled", err)
	}
}
