package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/hls"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/egoist/mygo"
)

// AppService 暴露应用级功能。以名字 "App" 绑定（main.go 里 BindAs），
// 前端通过 App.* 调用，避免与框架的 mygo.App 混淆。
type AppService struct{}

// Preview 是 ParseURL 的结果：新建任务前的探测预览。
type Preview struct {
	URL          string `json:"url"`
	Name         string `json:"name"`
	Size         int64  `json:"size"` // -1 表示未知
	AcceptRanges bool   `json:"acceptRanges"`
	ContentType  string `json:"contentType"`
	Kind         string `json:"kind"`      // http | hls
	MediaType    string `json:"mediaType"` // video | audio | other
}

// ParseURL 探测链接，返回可确认的预览信息。
func (a *AppService) ParseURL(ctx context.Context, url string) (Preview, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return Preview{}, fmt.Errorf("URL 不能为空")
	}
	probe, err := httpdl.Probe(ctx, url)
	if err != nil {
		return Preview{}, err
	}
	kind := "http"
	if hls.IsPlaylist(probe.URL, probe.ContentType) {
		kind = "hls"
	}
	return Preview{
		URL:          probe.URL,
		Name:         probe.FileName,
		Size:         probe.Size,
		AcceptRanges: probe.AcceptRanges,
		ContentType:  probe.ContentType,
		Kind:         kind,
		MediaType:    classifyMedia(probe.ContentType, probe.URL, kind),
	}, nil
}

// DownloadFfmpeg 手动触发 FFmpeg 获取（查找本机 → 自动下载并校验）。
// 阻塞直到完成；成功返回 ffmpeg 路径。
func (a *AppService) DownloadFfmpeg(ctx context.Context) (string, error) {
	return engine.EnsureFfmpeg(ctx, true)
}

// CheckFfmpeg 仅检测本机 FFmpeg（不触发下载），返回路径或空串。
func (a *AppService) CheckFfmpeg(ctx context.Context) (string, error) {
	return engine.FindFfmpeg(ctx), nil
}

// GetSettings 返回当前设置。
func (a *AppService) GetSettings() (Settings, error) {
	if settings == nil {
		return Settings{}, ErrNotReady
	}
	return settings.Get(), nil
}

// SaveSettings 保存设置，并同步队列并发数、下载代理与 FFmpeg 配置。
func (a *AppService) SaveSettings(v Settings) error {
	if settings == nil || mgr == nil {
		return ErrNotReady
	}
	// 先应用代理（含校验），失败则不落盘。
	if err := httpdl.SetProxy(v.Proxy); err != nil {
		return err
	}
	if err := settings.Set(v); err != nil {
		return err
	}
	mgr.SetMaxRun(v.MaxConcurrent)
	cacheDir, _ := mygo.App.Path(mygo.PathCache)
	engine.SetFfmpegOptions(v.FfmpegPath, filepath.Join(cacheDir, "ffmpeg"))
	UpdateSniffConfig() // 嗅探类型白名单即时推送给站点页面
	return nil
}

// MediaBase 返回 loopback 媒体服务 base URL（含 token），前端用它拼
// 文件与封面地址；服务器未就绪时返回空串。
func (a *AppService) MediaBase() (string, error) {
	base := MediaBase()
	if base == "" {
		return "", ErrNotReady
	}
	return base, nil
}

// PickFolder 弹出系统目录选择框；取消时返回空字符串。
func (a *AppService) PickFolder(ctx context.Context) (string, error) {
	dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            mygo.CallerWindow(ctx),
		Title:             "选择下载目录",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", nil
		}
		return "", err
	}
	if len(dirs) == 0 {
		return "", nil
	}
	return dirs[0], nil
}
