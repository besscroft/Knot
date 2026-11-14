// DASH 分轨下载：哔哩哔哩等站点的视频与音频是两路独立直链（fMP4），
// 本文件把两路分别多线程下载到工作目录（断点续传），再用 FFmpeg 流拷贝
// 合流为单个容器文件，复用 convert 的目标格式转换。
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/besscroft/knot/internal/proc"
)

// dashWorkDir 返回 DASH 任务的工作目录（分轨缓存）。
func dashWorkDir(t Task) string { return filepath.Join(t.Dir, ".knot-dash-"+t.ID) }

// runDash 执行一次 DASH 任务：下载视频/音频分轨 → FFmpeg 合流 → 按目标
// 格式转换 → 就位到 t.Path()。分轨文件按目标路径断点续传，全部成功后清理。
// 两路进度按探测到的字节数加权合并，对外仍是单个 Downloaded 计数。
func (m *Manager) runDash(ctx context.Context, e *entry, t Task) error {
	if t.URL == "" && t.AudioURL == "" {
		return fmt.Errorf("任务缺少下载地址")
	}
	work := dashWorkDir(t)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fmt.Errorf("无法创建工作目录: %w", err)
	}

	headers := taskHeaders(t)
	var firstDone, secondDone int64
	report := func() {
		m.mu.Lock()
		e.task.Downloaded = firstDone + secondDone
		m.mu.Unlock()
	}
	onShards := func(segs []httpdl.Segment) {
		shards := make([]ShardDetail, len(segs))
		for i, s := range segs {
			shards[i] = ShardDetail{
				Index: i, Start: s.Start, End: s.End, Written: s.Written,
				Active: s.Active, Retrying: s.Retrying,
			}
		}
		m.mu.Lock()
		e.shards = shards
		m.mu.Unlock()
	}

	if t.URL != "" {
		first := filepath.Join(work, "stream1.m4s")
		err := httpdl.Download(ctx, t.URL, first, httpdl.Options{
			Threads: t.Threads,
			Headers: headers,
			OnProgress: func(down, total int64) {
				firstDone = scaleProgress(down, total, t.StreamSize)
				report()
			},
			OnShards: onShards,
		})
		if err != nil {
			return err
		}
	}
	if t.AudioURL != "" {
		second := filepath.Join(work, "stream2.m4s")
		err := httpdl.Download(ctx, t.AudioURL, second, httpdl.Options{
			Threads: t.Threads,
			Headers: headers,
			OnProgress: func(down, total int64) {
				secondDone = scaleProgress(down, total, t.AudioSize)
				report()
			},
			OnShards: onShards,
		})
		if err != nil {
			return err
		}
	}
	return finalizeDash(ctx, t, work)
}

// finalizeDash 把分轨合流并就位：两路都在时 FFmpeg 封装为 MP4（流拷贝），
// 单路直接复用；目标格式非 mp4 时再走 convertWith 原地转换。
// 成功即清理工作目录；中途失败保留分轨供续传重试。
func finalizeDash(ctx context.Context, t Task, work string) error {
	first := filepath.Join(work, "stream1.m4s")
	second := filepath.Join(work, "stream2.m4s")
	hasFirst := t.URL != ""
	hasSecond := t.AudioURL != ""

	// 单流任务直接复用分轨文件；双流任务封装到 mux.mp4。
	muxPath := first
	if !hasFirst && hasSecond {
		muxPath = second
	}
	if hasFirst && hasSecond {
		// 合流需要 FFmpeg（缺失时自动下载，与格式转换一致）。
		ffmpeg, err := EnsureFfmpeg(ctx, true)
		if err != nil {
			return fmt.Errorf("合流需要 FFmpeg: %w", err)
		}
		muxPath = filepath.Join(work, "mux.mp4")
		if err := dashMux(ctx, ffmpeg, first, second, muxPath); err != nil {
			return err
		}
		// 分轨不再需要，但先不删目录：转换失败重试时无需重新下载。
		os.Remove(first)
		os.Remove(second)
	}

	// 目标格式：空/非法按 mp4（DASH 分轨本身是 fMP4，流拷贝即可）。
	format := strings.ToLower(strings.TrimSpace(t.Format))
	if !SupportedFormats[format] {
		format = "mp4"
	}
	if format != "mp4" && strings.TrimPrefix(filepath.Ext(muxPath), ".") != format {
		ffmpeg, err := EnsureFfmpeg(ctx, true)
		if err != nil {
			return fmt.Errorf("转换到 %s 需要 FFmpeg: %w", format, err)
		}
		if err := convertWith(ctx, ffmpeg, muxPath, format); err != nil {
			return err
		}
	}

	// 就位到最终文件名（Windows 上先删旧文件避免占用问题）。
	os.Remove(t.Path())
	if err := os.Rename(muxPath, t.Path()); err != nil {
		return err
	}
	_ = os.RemoveAll(work)
	return nil
}

// dashMux 用 FFmpeg 把视频/音频分轨无损封装为单个 MP4（流拷贝 + faststart，
// 音轨映射留容错：个别流可能缺音轨）。
func dashMux(ctx context.Context, ffmpeg, video, audio, dst string) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", video, "-i", audio,
		"-map", "0:v:0", "-map", "1:a:0?",
		"-c", "copy", "-movflags", "+faststart",
		"-f", "mp4", dst,
	}
	out, err := proc.CommandContext(c, ffmpeg, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // 暂停/取消：保持错误链供 manager 判定
		}
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("FFmpeg 合流失败: %s", detail)
	}
	return nil
}

// scaleProgress 把单流的实时进度换算为期望字节数（服务器报告的 total 与
// 建任务时的预估值不一致时按比例折算，避免进度跳变）。expected<=0 时不折算。
func scaleProgress(down, total, expected int64) int64 {
	if expected <= 0 || total <= 0 || total == expected {
		return down
	}
	scaled := down * expected / total
	if scaled > expected {
		scaled = expected
	}
	return scaled
}
