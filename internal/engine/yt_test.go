package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindYtOutput(t *testing.T) {
	dir := t.TempDir()
	// 中间产物应被排除；多个候选取修改时间最新的。
	files := []string{"标题.f137.mp4", "标题.f137.mp4.part", "标题.webm.ytdl", "别的.mp4"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(10 * time.Millisecond) // 保证修改时间可区分
	if err := os.WriteFile(filepath.Join(dir, "标题.mp4"), []byte("final"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := findYtOutput(Task{Dir: dir, Name: "标题.mp4"})
	if got != filepath.Join(dir, "标题.mp4") {
		t.Fatalf("findYtOutput = %q, want 标题.mp4", got)
	}
	// 只有中间产物：返回空。
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "标题.mp4.part"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := findYtOutput(Task{Dir: dir2, Name: "标题.mp4"}); got != "" {
		t.Fatalf("仅中间产物时应为空, got %q", got)
	}
}

func TestRelocateYtFileAlreadyInPlace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "标题.mp4"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := relocateYtFile(context.Background(), Task{Dir: dir, Name: "标题.mp4"}, "ffmpeg-fake"); err != nil {
		t.Fatalf("已在目标路径不应报错: %v", err)
	}
}

func TestCleanYtTemp(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"标题.mp4":           "final",
		"标题.mp4.part":      "part",
		"标题.f140.m4a":      "audio track",
		"标题.f140.m4a.part": "audio part",
		"标题.webm.ytdl":     "meta",
		"别的文件.mp4":         "other task", // 同目录其他任务/用户文件：不动
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cleanYtTemp(Task{Dir: dir, Name: "标题.mp4"})
	for name, want := range map[string]bool{
		"标题.mp4": true, "别的文件.mp4": true,
		"标题.mp4.part": false, "标题.f140.m4a": false, "标题.f140.m4a.part": false, "标题.webm.ytdl": false,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if want && err != nil {
			t.Errorf("%s 应保留: %v", name, err)
		}
		if !want && !os.IsNotExist(err) {
			t.Errorf("%s 应清理: %v", name, err)
		}
	}
}

func TestIsYtTempName(t *testing.T) {
	cases := []struct {
		suffix string
		want   bool
	}{
		{"mp4", false}, {"webm", false}, {"m4a", false},
		{"mp4.part", true}, {"f137.mp4", true}, {"f137.mp4.part", true},
		{"webm.ytdl", true}, {"m4a.ytdl", true},
	}
	for _, c := range cases {
		if got := isYtTempName(c.suffix); got != c.want {
			t.Errorf("isYtTempName(%q) = %v, want %v", c.suffix, got, c.want)
		}
	}
}
