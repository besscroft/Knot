// 数据目录迁移：mygo dev 默认用 "<名> Dev" 作为应用名，数据目录、缓存
// 与 WebView2 配置全部跟着应用名走，导致 dev 与构建产物各存一份，用户
// 换一边打开就像"数据全没了"。main.go 已用 SetName 统一应用名，这里在
// 启动时把旧开发目录里的数据做一次 best-effort 并入：仅当统一目录缺
// 该文件/目录时复制，绝不覆盖已有数据。浏览器 Cookie（WebView2 配置）
// 不迁移：体积大且两套配置并存时文件被锁，重新登录一次即可。
package service

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
)

// legacyDevSuffix 是 mygo dev 给应用名加的后缀。
const legacyDevSuffix = " Dev"

// migrateLegacyData 把 userData 与 cache 同级的 "<名> Dev" 旧目录并入
// 统一目录（任务、设置与 FFmpeg 缓存；封面缓存按需再生，不迁）。
func migrateLegacyData(userData, cache string) {
	if legacy := legacySibling(userData); legacy != "" {
		// tasks.json 允许覆盖空列表：统一目录刚创建时启动流程会先写入
		// "[]"，它不代表已有数据，不该挡住旧任务并入。
		if migrateFile(filepath.Join(userData, "tasks.json"), filepath.Join(legacy, "tasks.json"), isEmptyTaskFile) {
			log.Println("migrate: 已并入开发模式旧任务:", filepath.Join(legacy, "tasks.json"))
		}
		if migrateFile(filepath.Join(userData, "settings.json"), filepath.Join(legacy, "settings.json"), nil) {
			log.Println("migrate: 已并入开发模式旧设置:", filepath.Join(legacy, "settings.json"))
		}
	}
	if legacy := legacySibling(cache); legacy != "" {
		// FFmpeg 自动下载产物约百 MB，能省一次重复下载。
		if migrateTree(filepath.Join(cache, "ffmpeg"), filepath.Join(legacy, "ffmpeg")) {
			log.Println("migrate: 已并入开发模式的 FFmpeg 缓存")
		}
	}
}

// isEmptyTaskFile 报告内容是否为空任务列表（"[]"、空白或 null）。
func isEmptyTaskFile(data []byte) bool {
	switch string(bytes.TrimSpace(data)) {
	case "", "[]", "null":
		return true
	}
	return false
}

// legacySibling 返回 dir 同级的 "<名> Dev" 目录；不存在或非目录返回空串。
func legacySibling(dir string) string {
	if dir == "" {
		return ""
	}
	legacy := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+legacyDevSuffix)
	if info, err := os.Lstat(legacy); err != nil || !info.IsDir() {
		return ""
	}
	return legacy
}

// migrateFile 在 dst 缺失、或 dst 内容满足 allowReplace 时，用 src 覆盖
// （src 不可读则不动）。allowReplace 为 nil 时只在 dst 缺失时复制。
// 返回是否复制了。
func migrateFile(dst, src string, allowReplace func([]byte) bool) bool {
	var current []byte
	data, err := os.ReadFile(dst)
	if err == nil {
		if allowReplace == nil || !allowReplace(data) {
			return false // 已有数据不覆盖
		}
		current = data
	} else if !os.IsNotExist(err) {
		return false // 读取失败（占用等）：不动
	}
	fresh, err := os.ReadFile(src)
	if err != nil || string(fresh) == string(current) {
		return false
	}
	return os.WriteFile(dst, fresh, 0o644) == nil
}

// migrateTree 把 src 目录树并入 dst：dst 缺的文件逐个复制（含子目录），
// 已有的跳过。返回是否复制了任何文件。
func migrateTree(dst, src string) bool {
	entries, err := os.ReadDir(src)
	if err != nil {
		return false
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false
	}
	copied := false
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copied = migrateTree(d, s) || copied
			continue
		}
		copied = migrateFile(d, s, nil) || copied
	}
	return copied
}
