package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 支持的目标格式：视频容器（流拷贝）与音频格式（从视频提取或重编码）。
var SupportedFormats = map[string]bool{
	"mp4": true, "mkv": true, "mov": true, "webm": true,
	"m4a": true, "mp3": true, "opus": true, "flac": true, "wav": true,
}

// convertArgs 构建 ffmpeg 参数：优先流拷贝（不重编码，速度快）；
// 音频格式从视频源提取（-vn）。返回 (参数, 是否可能需要回退)。
func convertArgs(format string) ([]string, bool) {
	switch format {
	case "mkv":
		return []string{"-map", "0", "-c", "copy", "-f", "matroska"}, false
	case "mov":
		return []string{"-map", "0", "-c", "copy", "-f", "mov"}, false
	case "webm":
		return []string{"-map", "0", "-c", "copy", "-f", "webm"}, true // H264/AAC 无法放进 webm，需源编码兼容
	case "m4a":
		return []string{"-vn", "-c:a", "copy", "-f", "ipod"}, true // 源音频非 AAC 时回退重编码
	case "mp3":
		return []string{"-vn", "-c:a", "libmp3lame", "-q:a", "2", "-f", "mp3"}, false
	case "opus":
		return []string{"-vn", "-c:a", "libopus", "-b:a", "160k", "-f", "ogg"}, false
	case "flac":
		return []string{"-vn", "-c:a", "flac", "-f", "flac"}, false
	case "wav":
		return []string{"-vn", "-c:a", "pcm_s16le", "-f", "wav"}, false
	}
	return nil, false
}

// Finalize 把下载完成的文件转换为目标格式（format == "" 或与扩展名一致时跳过）。
// 返回最终文件路径（与输入相同——原文件被替换）。转换失败时保留原文件。
func Finalize(ctx context.Context, path, format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || !SupportedFormats[format] {
		return path, nil
	}
	if strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".") == format {
		return path, nil
	}

	ffmpeg, err := EnsureFfmpeg(ctx, true)
	if err != nil {
		return path, fmt.Errorf("转换到 %s 需要 FFmpeg: %w", format, err)
	}
	if err := convertWith(ctx, ffmpeg, path, format); err != nil {
		return path, err
	}
	return path, nil
}

// convertWith 用指定 ffmpeg 执行转换，输出先写临时文件再原子替换。
func convertWith(ctx context.Context, ffmpeg, path, format string) error {
	args, mayFallback := convertArgs(format)
	if args == nil {
		return fmt.Errorf("不支持的目标格式 %q", format)
	}
	tmp := path + "." + format + ".conv"
	if err := runFfmpeg(ctx, ffmpeg, path, tmp, args); err != nil && mayFallback {
		// 流拷贝失败（编码与目标容器不兼容）→ 回退重编码/重封装。
		var fallback []string
		switch format {
		case "webm":
			fallback = []string{"-map", "0:v:0", "-map", "0:a?", "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "34", "-c:a", "libopus", "-f", "webm"}
		case "m4a":
			fallback = []string{"-vn", "-c:a", "aac", "-b:a", "192k", "-f", "ipod"}
		default:
			fallback = nil
		}
		os.Remove(tmp)
		if fallback == nil {
			return err
		}
		if err2 := runFfmpeg(ctx, ffmpeg, path, tmp, fallback); err2 != nil {
			os.Remove(tmp)
			return err2
		}
	} else if err != nil {
		os.Remove(tmp)
		return err
	}

	// 替换原文件；Windows 上先删旧文件避免占用问题。
	os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

// runFfmpeg 跑一次转换，超时 2 小时（超长视频兜底）。
func runFfmpeg(ctx context.Context, ffmpeg, src, dst string, extra []string) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-i", src}
	args = append(args, extra...)
	args = append(args, dst)
	out, err := exec.CommandContext(c, ffmpeg, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // 暂停/取消：保持错误链供 manager 判定
		}
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("FFmpeg 转换失败: %s", detail)
	}
	return nil
}
