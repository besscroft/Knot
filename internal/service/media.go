package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/besscroft/knot/internal/engine"
	"github.com/egoist/mygo"
)

// 媒体协议：把已完成的任务文件与视频封面以 media:// 提供给前端，
// 供已完成页卡片展示与应用内播放。http.ServeContent 自动处理
// Range/206，视频拖动进度依赖它。handler 跑在独立 goroutine，可阻塞。
const (
	mediaScheme   = "media"
	thumbCacheDir = "thumbs" // PathCache 下的封面缓存目录名
)

var thumbMu sync.Mutex // 抽帧串行化：量小且比并发竞态简单

// RegisterMediaProtocol 注册 media 协议；须在窗口创建之前调用。
func RegisterMediaProtocol() {
	if err := mygo.Protocol.HandleFunc(mediaScheme, mediaHandler); err != nil {
		log.Println("注册 media 协议失败:", err)
	}
}

// mediaHandler 按 /file/<taskID> 与 /thumb/<taskID> 分发。
func mediaHandler(w http.ResponseWriter, r *http.Request) {
	if !fromMainWindow(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if mgr == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	task, ok := mgr.Get(parts[1])
	if !ok || task.Status != engine.StatusDone {
		http.NotFound(w, r)
		return
	}
	switch parts[0] {
	case "file":
		serveTaskFile(w, r, task)
	case "thumb":
		serveThumb(w, r, task)
	default:
		http.NotFound(w, r)
	}
}

// fromMainWindow 只服务主窗口（应用自身页面）：站点窗口是 Untrusted 的，
// 不能让它读取下载的文件。
func fromMainWindow(r *http.Request) bool {
	caller := mygo.CallerWindow(r.Context())
	main := MainWindow()
	return caller != nil && main != nil && caller == main
}

// serveTaskFile 直出任务的目标文件。
func serveTaskFile(w http.ResponseWriter, r *http.Request, task engine.Task) {
	f, err := os.Open(task.Path())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	// Content-Type 由扩展名推断；Range/条件请求由 ServeContent 处理。
	http.ServeContent(w, r, task.Name, st.ModTime(), f)
}

// serveThumb 返回视频封面：缓存未命中时用 ffmpeg 抽帧生成。
func serveThumb(w http.ResponseWriter, r *http.Request, task engine.Task) {
	if engine.MediaKind(task.Name) != "video" {
		http.NotFound(w, r)
		return
	}
	cacheDir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dst := filepath.Join(cacheDir, thumbCacheDir, task.ID+".jpg")

	if !exists(dst) {
		thumbMu.Lock()
		if !exists(dst) { // 等锁期间可能已被生成
			if err := extractThumb(task, dst); err != nil {
				thumbMu.Unlock()
				log.Println("生成封面失败:", task.Name, err)
				http.NotFound(w, r)
				return
			}
		}
		thumbMu.Unlock()
	}

	f, err := os.Open(dst)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "thumb.jpg", st.ModTime(), f)
}

// extractThumb 从视频抽一帧缩略图（10% 处或第 1 秒，缩放到 480 宽）。
func extractThumb(task engine.Task, dst string) error {
	ffmpeg := engine.FindFfmpeg(context.Background())
	if ffmpeg == "" {
		return fmt.Errorf("ffmpeg 不可用")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	at := "1"
	if task.Duration > 0 {
		at = strconv.FormatFloat(task.Duration*0.1, 'f', 1, 64)
	}
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, ffmpeg,
		"-y", "-v", "error", "-ss", at, "-i", task.Path(),
		"-frames:v", "1", "-vf", "scale=480:-2", "-q:v", "3", dst,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 抽帧失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// exists 报告路径存在且是普通文件。
func exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// removeThumb 清理任务的封面缓存（任务删除时调用）。
func removeThumb(taskID string) {
	if cacheDir, err := mygo.App.Path(mygo.PathCache); err == nil {
		_ = os.Remove(filepath.Join(cacheDir, thumbCacheDir, taskID+".jpg"))
	}
}
