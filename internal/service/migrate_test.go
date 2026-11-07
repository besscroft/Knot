package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyData(t *testing.T) {
	root := t.TempDir()
	userData := filepath.Join(root, "Knot")
	legacy := filepath.Join(root, "Knot Dev")
	cache := filepath.Join(root, "cache", "Knot")
	legacyCache := filepath.Join(root, "cache", "Knot Dev")
	for _, d := range []string{userData, legacy, filepath.Join(legacyCache, "ffmpeg")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(legacy, "tasks.json"), `[{"id":"abc","url":"https://x"}]`)
	write(filepath.Join(legacy, "settings.json"), `{"threads":16}`)
	write(filepath.Join(legacyCache, "ffmpeg", "ffmpeg.exe"), "fake-ffmpeg")
	// 模拟统一目录被启动流程先写过空列表：不应挡住旧任务并入。
	write(filepath.Join(userData, "tasks.json"), "[]")

	migrateLegacyData(userData, cache)

	// 任务与设置并入。
	if data, err := os.ReadFile(filepath.Join(userData, "tasks.json")); err != nil || string(data) != `[{"id":"abc","url":"https://x"}]` {
		t.Errorf("tasks.json 未并入: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(userData, "settings.json")); err != nil || string(data) != `{"threads":16}` {
		t.Errorf("settings.json 未并入: %q %v", data, err)
	}
	// FFmpeg 缓存并入。
	if data, err := os.ReadFile(filepath.Join(cache, "ffmpeg", "ffmpeg.exe")); err != nil || string(data) != "fake-ffmpeg" {
		t.Errorf("ffmpeg 缓存未并入: %q %v", data, err)
	}
}

func TestMigrateNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	userData := filepath.Join(root, "Knot")
	legacy := filepath.Join(root, "Knot Dev")
	for _, d := range []string{userData, legacy} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 已有真实任务数据不被覆盖。
	write(filepath.Join(userData, "tasks.json"), `[{"id":"new"}]`)
	write(filepath.Join(legacy, "tasks.json"), `[{"id":"old"}]`)

	migrateLegacyData(userData, "")

	if data, err := os.ReadFile(filepath.Join(userData, "tasks.json")); err != nil || string(data) != `[{"id":"new"}]` {
		t.Errorf("已有数据不应被覆盖: %q %v", data, err)
	}
}

func TestMigrateWithoutLegacy(t *testing.T) {
	root := t.TempDir()
	userData := filepath.Join(root, "Knot")
	if err := os.MkdirAll(userData, 0o755); err != nil {
		t.Fatal(err)
	}
	// 没有 "Knot Dev" 目录：不报错、不产生文件。
	migrateLegacyData(userData, "")
	if entries, err := os.ReadDir(userData); err != nil || len(entries) != 0 {
		t.Errorf("无旧数据时不应产生文件: %v %v", entries, err)
	}
}
