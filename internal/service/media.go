package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
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

// 媒体服务：把已完成的任务文件与视频封面提供给前端，供已完成页卡片
// 展示与应用内播放。必须走 loopback HTTP（127.0.0.1 随机端口 + 随机
// token 路径）而不是 mygo 自定义协议：Windows/WebView2 后端会把自定义
// 协议的整个响应体缓冲进内存（schemeResponse "WebView2 wants it whole"），
// 大视频一点播放就内存爆炸闪退；loopback 的 net/http 是真流式，
// http.ServeContent 的 Range/206 拖动照常可用，全平台行为一致
// （mygo 官方文档 frontend.md 对音视频正是这么建议的）。
const thumbCacheDir = "thumbs" // PathCache 下的封面缓存目录名

var (
	mediaMu      sync.Mutex
	mediaLn      net.Listener // loopback 监听；Shutdown 时关闭
	mediaToken   string       // URL 首段随机 token：本机其他进程不可猜测
	mediaBaseURL string       // 如 http://127.0.0.1:53462/<token>
)

var thumbMu sync.Mutex // 抽帧串行化：量小且比并发竞态简单

// StartMediaServer 启动 loopback 媒体服务器，并记录给前端的 base URL。
func StartMediaServer() {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		log.Println("生成媒体 token 失败:", err)
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Println("启动媒体服务器失败:", err)
		return
	}
	mediaMu.Lock()
	mediaLn = ln
	mediaToken = hex.EncodeToString(token)
	mediaBaseURL = "http://" + ln.Addr().String() + "/" + mediaToken
	mediaMu.Unlock()

	go func() {
		srv := &http.Server{Handler: http.HandlerFunc(mediaHandler)}
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Println("媒体服务器退出:", err)
		}
	}()
}

// StopMediaServer 关闭 loopback 监听（进程退出也会自然释放）。
func StopMediaServer() {
	mediaMu.Lock()
	defer mediaMu.Unlock()
	if mediaLn != nil {
		_ = mediaLn.Close()
		mediaLn = nil
	}
}

// MediaBase 返回媒体服务 base URL（含 token），失败返回空串。
func MediaBase() string {
	mediaMu.Lock()
	defer mediaMu.Unlock()
	return mediaBaseURL
}

// mediaHandler 按 /<token>/file/<taskID> 与 /<token>/thumb/<taskID> 分发。
// 鉴权只靠 token（loopback 上拿不到来源窗口）：token 每次启动随机生成、
// 只经 IPC 给到应用自己的页面，猜不出来即不可访问。
func mediaHandler(w http.ResponseWriter, r *http.Request) {
	mediaMu.Lock()
	token := mediaToken
	mediaMu.Unlock()
	if token == "" {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != token || parts[2] == "" {
		http.NotFound(w, r)
		return
	}
	if mgr == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	task, ok := mgr.Get(parts[2])
	if !ok || task.Status != engine.StatusDone {
		http.NotFound(w, r)
		return
	}
	switch parts[1] {
	case "file":
		serveTaskFile(w, r, task)
	case "thumb":
		serveThumb(w, r, task)
	default:
		http.NotFound(w, r)
	}
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
	// 封面生成后不变，可放心缓存。
	w.Header().Set("Cache-Control", "private, max-age=86400")
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
