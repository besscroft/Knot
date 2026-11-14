package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/proc"
	"github.com/egoist/mygo"
)

// 媒体服务：把已完成的任务文件与视频封面提供给前端，供已完成页卡片
// 展示与应用内播放；同时托管 Web 文件库页面（web/index.html，内嵌），
// 供本机/局域网浏览器免 token 直接查看、播放与下载所有已完成的文件。
// 必须走 HTTP 而不是 mygo 自定义协议：Windows/WebView2 后端会把自定义
// 协议的整个响应体缓冲进内存（schemeResponse "WebView2 wants it whole"），
// 大视频一点播放就内存爆炸闪退；net/http 是真流式，http.ServeContent
// 的 Range/206 拖动照常可用，全平台行为一致
// （mygo 官方文档 frontend.md 对音视频正是这么建议的）。
//
// 访问控制（无 token：手机浏览器输 ip:端口 即可打开）：默认只绑
// 127.0.0.1:<webPort>，设置开启局域网访问才绑 0.0.0.0:<webPort>；
// 同时校验 Host 白名单（仅本机/局域网地址），防恶意网页借 DNS
// rebinding 跨域读取文件列表。
const thumbCacheDir = "thumbs" // PathCache 下的封面缓存目录名

//go:embed web/index.html
var webIndex []byte

var (
	mediaMu      sync.Mutex
	mediaLn      net.Listener // 监听；Shutdown 时关闭
	mediaPort    string       // 实际监听端口（绑定失败回退随机后与设置不同）
	mediaLanIP   string       // 启动时缓存的局域网 IPv4（供 WebPageURL 展示）
	mediaBaseURL string       // 如 http://127.0.0.1:22333
)

var thumbMu sync.Mutex // 抽帧串行化：量小且比并发竞态简单

// StartMediaServer 启动媒体服务器，并记录给前端的 base URL。
// 端口用设置的 webPort（默认 22333，本机与局域网共用）；绑定失败回退
// 随机端口。默认只绑 127.0.0.1，设置开启局域网访问才绑 0.0.0.0。
func StartMediaServer() {
	port := DefaultWebPort
	access := false
	if settings != nil {
		v := settings.Get()
		port = v.WebPort
		access = v.WebAccess
	}
	host := "127.0.0.1"
	if access {
		host = "0.0.0.0"
	}
	ln, err := net.Listen("tcp", host+":"+strconv.Itoa(port))
	if err != nil {
		log.Printf("监听 %s 失败，回退本机随机端口: %v", host, err)
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		log.Println("启动媒体服务器失败:", err)
		return
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	mediaMu.Lock()
	mediaLn = ln
	mediaPort = portStr
	mediaLanIP = lanIPv4()
	// 应用内固定用 127.0.0.1 访问：监听 0.0.0.0 时 WebView 未必能解析该主机名。
	mediaBaseURL = "http://127.0.0.1:" + portStr
	mediaMu.Unlock()

	go func() {
		srv := &http.Server{Handler: http.HandlerFunc(mediaHandler)}
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Println("媒体服务器退出:", err)
		}
	}()
}

// RestartMediaServer 按最新设置重启媒体服务器（监听方式/端口变化后调用）。
func RestartMediaServer() {
	StopMediaServer()
	StartMediaServer()
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

// MediaBase 返回媒体服务 base URL，失败返回空串。
func MediaBase() string {
	mediaMu.Lock()
	defer mediaMu.Unlock()
	return mediaBaseURL
}

// WebPageURL 返回 Web 文件库页面地址（含结尾斜杠）；服务器未就绪返回
// 空串。开启局域网访问时用本机局域网 IP，方便其他设备直接使用。
func WebPageURL() string {
	mediaMu.Lock()
	base, port, lanIP := mediaBaseURL, mediaPort, mediaLanIP
	mediaMu.Unlock()
	if base == "" {
		return ""
	}
	if settings != nil && settings.Get().WebAccess && port != "" && lanIP != "" {
		return "http://" + net.JoinHostPort(lanIP, port) + "/"
	}
	return base + "/"
}

// hostAllowed 校验 Host 白名单（防 DNS rebinding：恶意网页把自己的域名
// 解析到本服务器后，浏览器会带上攻击者域名的 Host，直接拒绝）。放行
// IP 字面量直连（任意本机网卡的 IP，手机/电脑输 ip:端口 即属此类）与
// localhost 系名称；域名一律拒绝。空 Host（HTTP/1.0）也放行。
func hostAllowed(host string) bool {
	if host == "" {
		return true
	}
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i] // 去掉端口（IPv6 方括号形式不受影响）
	}
	h = strings.ToLower(strings.Trim(h, "[]"))
	switch h {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return net.ParseIP(h) != nil
}

// lanIPv4 返回本机局域网 IPv4（优先 192.168 网段，避免拿到虚拟网卡的
// 地址），找不到任何非回环 IPv4 返回空串。仅用于展示访问地址。
func lanIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	other := ""
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		s := ipn.IP.String()
		if strings.HasPrefix(s, "192.168.") {
			return s
		}
		if other == "" {
			other = s
		}
	}
	return other
}

// mediaHandler 分发：/ 为 Web 文件库页面，/api/list 为文件列表 JSON，
// /file|thumb/<taskID> 为文件与封面。访问控制靠绑定地址 + Host 白名单
// （见 hostAllowed）：默认仅本机，开启局域网访问后同一网络可直接访问。
func mediaHandler(w http.ResponseWriter, r *http.Request) {
	if !hostAllowed(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	// 页面与列表不需要定位具体任务，提前分流。
	switch {
	case len(parts) == 1 && parts[0] == "":
		serveWebIndex(w, r)
		return
	case len(parts) == 2 && parts[0] == "api" && parts[1] == "list":
		serveWebList(w, r)
		return
	case len(parts) == 2 && parts[0] == "api" && parts[1] == "ping":
		serveAPIPing(w, r)
		return
	case len(parts) == 2 && parts[0] == "api" && parts[1] == "create":
		handleAPICreate(w, r)
		return
	case len(parts) == 2 && parts[0] == "ext" && parts[1] == "updates.xml":
		serveExtUpdates(w, r)
		return
	case len(parts) == 2 && parts[0] == "ext" && parts[1] == "knot-extension.crx":
		serveExtCrx(w, r)
		return
	}
	if mgr == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
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

// serveWebIndex 返回 Web 文件库页面：静态自包含 HTML，随二进制内嵌。
func serveWebIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 页面脚本样式内联；数据请求与资源只放行同源（connect-src 管列表 fetch，
	// 缺了它 default-src 'none' 会把同源 fetch 一并拦掉）。
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; connect-src 'self'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(webIndex)
}

// webItem 是 Web 文件库列表项：只暴露展示所需的最小字段。
type webItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`        // video | audio | image | other
	Size        int64   `json:"size"`        // 磁盘实际大小（转换会变），缺失为 0
	Duration    float64 `json:"duration"`    // 秒；未知为 0
	CompletedAt string  `json:"completedAt"` // RFC3339；缺失为空
	Missing     bool    `json:"missing"`     // 文件已被外部删除

	completed int64 // 完成时间 Unix 秒，仅排序用
}

// serveWebList 返回所有已完成任务的 JSON 列表，按完成时间倒序。
func serveWebList(w http.ResponseWriter, r *http.Request) {
	if mgr == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	items := make([]webItem, 0)
	for _, t := range mgr.List() {
		if t.Status != engine.StatusDone {
			continue
		}
		item := webItem{
			ID:       t.ID,
			Name:     t.Name,
			Kind:     engine.MediaKind(t.Name),
			Duration: t.Duration,
		}
		if st, err := os.Stat(t.Path()); err == nil && !st.IsDir() {
			item.Size = st.Size()
		} else {
			item.Missing = true
		}
		if t.CompletedAt != nil {
			item.completed = t.CompletedAt.Unix()
			item.CompletedAt = t.CompletedAt.Format(time.RFC3339)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].completed > items[j].completed })
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(items)
}

// serveTaskFile 直出任务的目标文件；?dl=1 时强制另存为（Web 文件库
// 的下载按钮用），否则按扩展名内联播放/展示。
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
	if r.URL.Query().Get("dl") == "1" {
		// RFC 5987 编码：任务名可能含中文等非 ASCII 字符。
		w.Header().Set("Content-Disposition",
			"attachment; filename*=UTF-8''"+url.PathEscape(task.Name))
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
	cmd := proc.CommandContext(c, ffmpeg,
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
