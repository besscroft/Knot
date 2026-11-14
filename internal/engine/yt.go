// yt-dlp 任务（kind=yt）：解析、下载、合流全权交给 yt-dlp 子进程。YouTube
// 的直链带签名时效且反爬多变，抽取直链自下载不可持续；这里只负责二进制
// 就绪、参数装配、进度转写与产物归位。断点续传由 yt-dlp 的 .part 机制
// 承担：暂停即终止进程，恢复重跑自动续传。
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/besscroft/knot/internal/engine/ytdlp"
)

// runYt 执行一次 yt-dlp 任务。
func (m *Manager) runYt(ctx context.Context, e *entry, t Task) error {
	bin, err := ytdlp.Ensure(ctx, true)
	if err != nil {
		return err
	}
	// 合流/音频提取都需要 FFmpeg，缺失时自动下载（与 DASH 合流一致）。
	ffmpeg, err := EnsureFfmpeg(ctx, true)
	if err != nil {
		return fmt.Errorf("合流需要 FFmpeg: %w", err)
	}
	base := strings.TrimSuffix(t.Name, filepath.Ext(t.Name))
	err = ytdlp.Download(ctx, bin, ytdlp.DownloadOptions{
		URL:      t.URL,
		Dir:      t.Dir,
		Base:     base,
		Format:   t.Format,
		Selector: t.Selector,
		Ffmpeg:   ffmpeg,
		Proxy:    httpdl.ProxyTarget(),
		OnProgress: func(down int64) {
			m.mu.Lock()
			e.task.Downloaded = down
			if down > e.task.Total {
				// 解析时的估算可能偏小：抬到实际值，防止进度超过 100%。
				e.task.Total = down
			}
			m.mu.Unlock()
		},
	})
	if err != nil {
		return err
	}
	if err := relocateYtFile(ctx, t, ffmpeg); err != nil {
		return err
	}
	// 按落盘文件校准总量（估算值与实际可能偏差几个百分点），runTask 的
	// 完成分支会把 Downloaded 对齐到 Total。
	if info, err := os.Stat(t.Path()); err == nil && info.Size() > 0 {
		m.mu.Lock()
		e.task.Total = info.Size()
		m.mu.Unlock()
	}
	return nil
}

// relocateYtFile 把 yt-dlp 产物对齐到目标路径。产物已在目标路径时直接
// 返回；否则按前缀定位产物，扩展名与目标格式不一致（合流回退到渐进式
// 单文件、mov 等非原生合流容器）时先用 FFmpeg 转换再改名——runTask 的
// Finalize 按扩展名判断是否需要转换，内容不符时会被跳过，真正的转换
// 必须在这里完成。ffmpeg 由调用方解析（runYt 已 Ensure），为空时兜底查找。
func relocateYtFile(ctx context.Context, t Task, ffmpeg string) error {
	if _, err := os.Stat(t.Path()); err == nil {
		return nil
	}
	best := findYtOutput(t)
	if best == "" {
		return fmt.Errorf("yt-dlp 未产出目标文件")
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(t.Name), "."))
	if SupportedFormats[format] &&
		strings.TrimPrefix(strings.ToLower(filepath.Ext(best)), ".") != format {
		if ffmpeg == "" {
			var err error
			ffmpeg, err = EnsureFfmpeg(ctx, true)
			if err != nil {
				return fmt.Errorf("转换到 %s 需要 FFmpeg: %w", format, err)
			}
		}
		if err := convertWith(ctx, ffmpeg, best, format); err != nil {
			return err
		}
	}
	os.Remove(t.Path())
	return os.Rename(best, t.Path())
}

// findYtOutput 按目标名前缀找到 yt-dlp 的最终产物：跳过中间产物
// （.part/.ytdl/分轨），取修改时间最新者；没有则返回空串。
func findYtOutput(t Task) string {
	base := strings.TrimSuffix(t.Name, filepath.Ext(t.Name)) + "."
	entries, err := os.ReadDir(t.Dir)
	if err != nil {
		return ""
	}
	var best string
	var bestMod time.Time
	for _, en := range entries {
		if en.IsDir() || !strings.HasPrefix(en.Name(), base) {
			continue
		}
		if isYtTempName(strings.TrimPrefix(en.Name(), base)) {
			continue
		}
		fi, err := en.Info()
		if err != nil {
			continue
		}
		if best == "" || fi.ModTime().After(bestMod) {
			best, bestMod = filepath.Join(t.Dir, en.Name()), fi.ModTime()
		}
	}
	return best
}

// cleanYtTemp 清理 yt 任务残留的中间产物（暂停/出错/删除后可能留下
// .part 断点、.ytdl 元数据与分轨中间文件）。
func cleanYtTemp(t Task) {
	base := strings.TrimSuffix(t.Name, filepath.Ext(t.Name)) + "."
	entries, err := os.ReadDir(t.Dir)
	if err != nil {
		return
	}
	for _, en := range entries {
		if en.IsDir() || !strings.HasPrefix(en.Name(), base) {
			continue
		}
		if isYtTempName(strings.TrimPrefix(en.Name(), base)) {
			_ = os.Remove(filepath.Join(t.Dir, en.Name()))
		}
	}
}

// ytFragRe 匹配分轨中间文件的后缀（f<数字>.<扩展名>，可带 .part）。
var ytFragRe = regexp.MustCompile(`^f\d+\.\w+(\.part)?$`)

// isYtTempName 判断目标名后缀是否为 yt-dlp 的中间产物（.part 断点文件、
// .ytdl 续传元数据、f<数字> 分轨中间文件）：产物归位不把中间产物当最终
// 文件，Remove 清理也用它识别。
func isYtTempName(suffix string) bool {
	switch {
	case strings.HasSuffix(suffix, ".part"),
		strings.Contains(suffix, ".ytdl"),
		strings.HasSuffix(suffix, ".temp"):
		return true
	}
	return ytFragRe.MatchString(suffix)
}
