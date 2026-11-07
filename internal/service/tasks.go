package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/hls"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/egoist/mygo"
)

// Tasks 是暴露给前端的下载任务服务（frontend/src/mygo.ts 中的 Tasks.*）。
type Tasks struct{}

// CreateOptions 是新建任务的参数，均可省略。
type CreateOptions struct {
	Dir     string `json:"dir,omitempty"`     // 保存目录，空 = 设置里的下载目录
	Name    string `json:"name,omitempty"`    // 文件名，空 = 从响应/URL 推断
	Threads int    `json:"threads,omitempty"` // 分片数，0 = 用设置
	Format  string `json:"format,omitempty"`  // 目标格式，空 = 用设置默认/原样
	// 浏览上下文：应用内浏览器创建任务时携带，规避防盗链。
	Referer   string `json:"referer,omitempty"`
	Cookie    string `json:"cookie,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
}

// headers 把上下文转换为引擎请求头。
func (o CreateOptions) headers() *httpdl.Headers {
	if o.Referer == "" && o.Cookie == "" && o.UserAgent == "" {
		return nil
	}
	return &httpdl.Headers{Referer: o.Referer, Cookie: o.Cookie, UserAgent: o.UserAgent}
}

// Create 探测 URL 并创建下载任务，返回带 ID 的任务。
func (t *Tasks) Create(ctx context.Context, url string, opt CreateOptions) (engine.Task, error) {
	return createTask(ctx, url, opt)
}

// createTask 是创建任务的共享实现：探测 → 识别类型 → 定名 → 入队。
// 浏览器窗口的下载拦截与手动下载也走这里。
func createTask(ctx context.Context, url string, opt CreateOptions) (engine.Task, error) {
	if mgr == nil || settings == nil {
		return engine.Task{}, ErrNotReady
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return engine.Task{}, fmt.Errorf("URL 不能为空")
	}

	probe, err := httpdl.ProbeWithHeaders(ctx, url, opt.headers())
	if err != nil {
		return engine.Task{}, err
	}

	s := settings.Get()
	dir := opt.Dir
	if dir == "" {
		dir = s.DownloadDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return engine.Task{}, fmt.Errorf("无法创建下载目录: %w", err)
	}

	// m3u8 → HLS 任务：解析 master 选最高清晰度，产出 .mp4。
	if hls.IsPlaylist(probe.URL, probe.ContentType) {
		return createHLSTask(ctx, probe, dir, opt)
	}

	media := classifyMedia(probe.ContentType, probe.URL, "http")
	format := resolveFormat(opt.Format, media, s)

	name := opt.Name
	if name == "" {
		name = probe.FileName
	}
	if format != "" {
		name = swapExt(name, format)
	}
	name = uniqueName(dir, name)

	threads := opt.Threads
	if threads == 0 {
		threads = s.Threads
	}

	return mgr.Create(engine.Task{
		URL:       probe.URL,
		Name:      name,
		Dir:       dir,
		Total:     probe.Size,
		Threads:   threads,
		Format:    format,
		Referer:   opt.Referer,
		Cookie:    opt.Cookie,
		UserAgent: opt.UserAgent,
	})
}

// classifyMedia 从 Content-Type/URL 推断媒体类别（video/audio/other）。
func classifyMedia(contentType, rawURL, kind string) string {
	if kind == "hls" {
		return "video"
	}
	mt := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(mt, "video/"):
		return "video"
	case strings.HasPrefix(mt, "audio/"):
		return "audio"
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(strings.Split(rawURL, "?")[0]), "."))
	switch ext {
	case "mp4", "mkv", "mov", "webm", "avi", "flv", "ts", "m4v":
		return "video"
	case "mp3", "m4a", "aac", "flac", "wav", "opus", "ogg":
		return "audio"
	}
	return "other"
}

// resolveFormat 决定任务的目标格式：显式指定 > 设置默认（按媒体类别）> 原样。
func resolveFormat(explicit, media string, s Settings) string {
	f := strings.ToLower(strings.TrimSpace(explicit))
	if f != "" && engine.SupportedFormats[f] {
		return f
	}
	switch media {
	case "audio":
		return s.AudioFormat
	case "video":
		return s.VideoFormat
	}
	return ""
}

// swapExt 把文件名的扩展名换成目标格式。
func swapExt(name, format string) string {
	if format == "" {
		return name
	}
	base := name
	if i := strings.LastIndexByte(name, '.'); i > 0 && i > len(name)-8 {
		base = name[:i]
	}
	return base + "." + format
}

// createHLSTask 解析 master playlist 并登记 HLS 任务。
func createHLSTask(ctx context.Context, probe *httpdl.ProbeResult, dir string, opt CreateOptions) (engine.Task, error) {
	mediaURL, err := hls.Resolve(ctx, httpdl.Client(), probe.URL, opt.headers())
	if err != nil {
		return engine.Task{}, err
	}

	s := settings.Get()
	format := resolveFormat(opt.Format, "video", s)
	if format == "" {
		format = "mp4" // HLS 必须落一个容器
	}

	name := opt.Name
	if name == "" {
		// index.m3u8 这类名字没有信息量：去掉扩展名换目标容器。
		base := path.Base(strings.Split(probe.URL, "?")[0])
		name = strings.TrimSuffix(base, path.Ext(base))
		if name == "" || name == "/" || name == "." {
			name = "video"
		}
		name += "." + format
	} else {
		name = swapExt(name, format)
	}
	name = uniqueName(dir, name)

	threads := opt.Threads
	if threads == 0 {
		threads = s.Threads
	}

	return mgr.Create(engine.Task{
		Kind:      "hls",
		URL:       mediaURL,
		Name:      name,
		Dir:       dir,
		Total:     -1, // 分片总大小未知，进度按分片数展示
		Threads:   threads,
		Format:    format,
		Referer:   opt.Referer,
		Cookie:    opt.Cookie,
		UserAgent: opt.UserAgent,
	})
}

// List 返回全部任务（按创建顺序）。
func (t *Tasks) List() ([]engine.Task, error) {
	if mgr == nil {
		return nil, ErrNotReady
	}
	return mgr.List(), nil
}

// Shards 返回任务最近的分片快照（任务详情框每秒轮询）。
// 仅 http 多线程任务有数据；HLS 返回空（用任务上的分片聚合计数）。
func (t *Tasks) Shards(id string) ([]engine.ShardDetail, error) {
	if mgr == nil {
		return nil, ErrNotReady
	}
	return mgr.Shards(id), nil
}

// Pause 暂停任务。
func (t *Tasks) Pause(id string) error {
	if mgr == nil {
		return ErrNotReady
	}
	return mgr.Pause(id)
}

// Resume 恢复 paused 或 error 状态的任务。
func (t *Tasks) Resume(id string) error {
	if mgr == nil {
		return ErrNotReady
	}
	return mgr.Resume(id)
}

// Remove 删除任务记录并清理断点文件；deleteFiles 为真时连同磁盘上的目标文件一并删除。
func (t *Tasks) Remove(id string, deleteFiles bool) error {
	if mgr == nil {
		return ErrNotReady
	}
	task, ok := mgr.Get(id)
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	if err := mgr.Remove(id); err != nil {
		return err
	}
	removeThumb(id) // 封面缓存一并清理
	if deleteFiles {
		// 记录已删除，文件若正被占用等删不掉时尽力而为，不阻塞前端移除记录。
		_ = os.Remove(task.Path())
	}
	return nil
}

// OpenPath 用系统默认程序打开任务的目标文件。
func (t *Tasks) OpenPath(id string) error {
	if mgr == nil {
		return ErrNotReady
	}
	task, ok := mgr.Get(id)
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	return mygo.Shell.OpenPath(task.Path())
}

// Reveal 在系统的文件管理器中显示任务的目标文件。
func (t *Tasks) Reveal(id string) error {
	if mgr == nil {
		return ErrNotReady
	}
	task, ok := mgr.Get(id)
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	mygo.Shell.ShowItemInFolder(task.Path())
	return nil
}

// uniqueName 在 dir 内为 name 找一个不重名的名字："a.zip" → "a (1).zip"。
func uniqueName(dir, name string) string {
	base := name
	ext := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		base, ext = name[:i], name[i:]
	}
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		if _, err := os.Lstat(filepath.Join(dir, candidate)); err != nil {
			// 不存在即采用；无法检查也采用，留待真正创建文件时报错。
			return candidate
		}
	}
}
