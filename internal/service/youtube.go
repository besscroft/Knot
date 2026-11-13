// YouTube 视频支持：解析、画质选择与下载全权交给 yt-dlp（外部二进制，
// 见 engine/ytdlp）。粘贴链接与应用内浏览器嗅探两个入口共用这里的解析
// 与创建逻辑；解析结果短缓存，弹窗→创建的两次调用共享同一份格式信息，
// 避免重复跑 yt-dlp -J。
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/besscroft/knot/internal/engine/ytdlp"
)

// YouTubeInfo 是 YouTube 视频的解析结果（App.ResolveYouTube 返回值）。
type YouTubeInfo struct {
	Title     string           `json:"title"`
	Duration  float64          `json:"duration"` // 秒，0 = 未知
	Qualities []YouTubeQuality `json:"qualities"`
}

// YouTubeQuality 是一个可选画质档位；大小为视频+音轨的估算字节数。
type YouTubeQuality struct {
	ID     int    `json:"id"`               // 档位值（短边像素），0 = 最佳
	Desc   string `json:"desc"`             // 如 "1080p60"
	Codecs string `json:"codecs,omitempty"` // 兼容性最好的编码
	Size   int64  `json:"size"`             // 估算字节数，0 = 未知
}

// ytHosts 是 YouTube 的各站点域名（ParseURL 用它把整域路由进 yt-dlp
// 解析，播放列表页等由 resolve 给出明确的暂不支持错误）。
var ytHosts = map[string]bool{
	"www.youtube.com": true, "m.youtube.com": true, "music.youtube.com": true,
	"youtube.com": true, "youtu.be": true, "www.youtu.be": true,
}

// isYouTubeSite 报告 URL 是否属于 YouTube 域。
func isYouTubeSite(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return ytHosts[strings.ToLower(u.Hostname())]
}

// isYouTubeVideoPage 报告 URL 是否为 YouTube 单视频页（watch / shorts /
// live 回放 / youtu.be 短链）。播放列表页不算。
func isYouTubeVideoPage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !ytHosts[strings.ToLower(u.Hostname())] {
		return false
	}
	if strings.EqualFold(u.Hostname(), "youtu.be") {
		p := strings.Trim(u.Path, "/")
		return p != "" && !strings.Contains(p, "/")
	}
	switch {
	case strings.HasPrefix(u.Path, "/watch"):
		return u.Query().Get("v") != ""
	case strings.HasPrefix(u.Path, "/shorts/"), strings.HasPrefix(u.Path, "/live/"):
		return true
	}
	return false
}

// ytResolveCache 是最近一次解析的缓存（单槽，键为链接原串）。弹窗解析
// 与创建任务间隔通常只有几秒，重新跑 yt-dlp -J 纯属浪费。
var ytResolveCache struct {
	mu   sync.Mutex
	url  string
	at   time.Time
	info *ytdlp.Info
}

// resolveYouTube 解析 YouTube 视频（带短缓存）。播放列表与直播已在
// ytdlp.Resolve 中给出明确错误。
func resolveYouTube(ctx context.Context, rawURL string) (*ytdlp.Info, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !isYouTubeSite(rawURL) {
		return nil, errors.New("不是 YouTube 视频链接")
	}
	ytResolveCache.mu.Lock()
	if ytResolveCache.info != nil && ytResolveCache.url == rawURL &&
		time.Since(ytResolveCache.at) < 5*time.Minute {
		info := ytResolveCache.info
		ytResolveCache.mu.Unlock()
		return info, nil
	}
	ytResolveCache.mu.Unlock()

	bin, err := ytdlp.Ensure(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("解析需要 yt-dlp: %w", err)
	}
	info, err := ytdlp.Resolve(ctx, bin, rawURL, httpdl.ProxyTarget())
	if err != nil {
		return nil, err
	}
	if info.Live {
		return nil, errors.New("直播流暂不支持下载")
	}
	if len(info.Qualities) == 0 {
		return nil, errors.New("未能获取到可用画质（视频可能受限或不支持下载）")
	}
	ytResolveCache.mu.Lock()
	ytResolveCache.url, ytResolveCache.at, ytResolveCache.info = rawURL, time.Now(), info
	ytResolveCache.mu.Unlock()
	return info, nil
}

// ResolveYouTube 解析 YouTube 链接的标题与可选画质（新建下载弹窗用）。
func (a *AppService) ResolveYouTube(ctx context.Context, url string) (YouTubeInfo, error) {
	info, err := resolveYouTube(ctx, url)
	if err != nil {
		return YouTubeInfo{}, err
	}
	out := YouTubeInfo{
		Title:     info.Title,
		Duration:  info.Duration,
		Qualities: make([]YouTubeQuality, 0, len(info.Qualities)),
	}
	for _, q := range info.Qualities {
		out.Qualities = append(out.Qualities, YouTubeQuality{
			ID: q.ID, Desc: q.Desc, Codecs: q.Codecs, Size: q.Size,
		})
	}
	return out, nil
}

// CreateYouTube 把 YouTube 视频加入下载队列：qualityID 为解析返回的画质
// 档位；format 语义与 Tasks.Create 一致（音频格式 = 仅提取音轨）；
// name 为空时用视频标题。
func (t *Tasks) CreateYouTube(ctx context.Context, url string, qualityID int, opt CreateOptions) (engine.Task, error) {
	if mgr == nil || settings == nil {
		return engine.Task{}, ErrNotReady
	}
	info, err := resolveYouTube(ctx, url)
	if err != nil {
		return engine.Task{}, err
	}

	s := settings.Get()
	// 目标格式：显式指定 > 设置默认 > 容器兜底（合流必须落一个容器）。
	f := strings.ToLower(strings.TrimSpace(opt.Format))
	if isAudioFormat(f) {
		if f = resolveFormat(f, "audio", s); f == "" {
			f = "m4a"
		}
	} else {
		if f = resolveFormat(f, "video", s); f == "" {
			f = "mp4"
		}
	}

	q := info.Quality(qualityID)
	selector := info.FormatSelector(qualityID, isAudioFormat(f))
	desc := ""
	if q != nil {
		desc = q.Desc
	}

	dir := s.DownloadDir
	if err := mkTaskDir(&dir); err != nil {
		return engine.Task{}, err
	}
	name := opt.Name
	if name == "" {
		name = ytDefaultName(info)
	}
	name = swapExt(name, f)
	name = uniqueName(dir, name)

	// 大小估算（视频+音轨，未知为 -1）；实际大小在完成时按落盘文件校准。
	total := int64(-1)
	if q != nil && q.Size > 0 {
		total = q.Size
	}

	return mgr.Create(engine.Task{
		Kind:     "yt",
		URL:      strings.TrimSpace(url),
		Name:     name,
		Dir:      dir,
		Total:    total,
		Threads:  s.Threads,
		Format:   f,
		Quality:  desc,
		Selector: selector,
	})
}

// ytDefaultName 从视频标题生成默认文件名。
func ytDefaultName(info *ytdlp.Info) string {
	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = "youtube视频"
	}
	return sanitizeTaskName(title)
}
