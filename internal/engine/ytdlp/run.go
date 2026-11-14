// yt-dlp 下载执行：装配命令行参数、扫描进程输出解析进度。进度跨视频/
// 音频两阶段累加（yt-dlp 每阶段从 0 重新计数），对外是单个累计字节数。
package ytdlp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/besscroft/knot/internal/proc"
)

// progressMark 是进度模板的前缀标记：--progress-template 的 download 模板
// 固定以它开头，输出里与 yt-dlp 的日志行天然区分。
const progressMark = "KNP|"

// DownloadOptions 是一次 yt-dlp 下载的装配参数。
type DownloadOptions struct {
	Dir      string // 输出目录（-P）
	Base     string // 目标文件名（不含扩展名）
	Format   string // 目标容器/音频格式（mp4/mkv/mov/webm 或 m4a/mp3/opus/flac/wav）
	Selector string // -f 格式选择表达式（Info.FormatSelector 组装）
	Ffmpeg   string // ffmpeg 路径（合流/提取需要），空 = 交给 yt-dlp 自查
	Proxy    string // 代理地址，空 = 跟随环境变量
	// OnProgress 收到累计下载字节（跨阶段累加），在独立 goroutine 中回调，
	// 实现方须自行保证并发安全。
	OnProgress func(down int64)
}

// DownloadArgs 组装 yt-dlp 下载参数。输出模板固定为 "base.%(ext)s"：
// 合流容器的扩展名与 Format 一致；个别回退场景（合流回退到渐进式单文件）
// 扩展名可能漂移，由引擎侧把产物归位到目标路径。
func DownloadArgs(o DownloadOptions) []string {
	base := strings.ReplaceAll(o.Base, "%", "%%") // yt-dlp 模板里 % 有特殊语义
	args := []string{
		"--no-playlist",
		"--newline", // 进度按整行输出，便于按行解析
		"--progress-template", "download:" + progressMark +
			"%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s",
		"-f", o.Selector,
	}
	if isAudioTarget(o.Format) {
		// 音频容器：提取并按需重编码（m4a 源即 m4a 时为流拷贝）。
		args = append(args, "-x", "--audio-format", o.Format)
	} else {
		// 视频容器：mp4/mkv/webm 原生合流；mov 等先落 mp4，引擎侧再转换。
		merge := o.Format
		if merge != "mp4" && merge != "mkv" && merge != "webm" {
			merge = "mp4"
		}
		args = append(args, "--merge-output-format", merge)
	}
	if o.Ffmpeg != "" {
		args = append(args, "--ffmpeg-location", o.Ffmpeg)
	}
	if o.Proxy != "" {
		args = append(args, "--proxy", o.Proxy)
	}
	return append(args, "-P", o.Dir, "-o", base+".%(ext)s")
}

// Download 执行 yt-dlp 下载（阻塞到进程退出）。ctx 取消会终止进程
// （暂停/删除），断点由 yt-dlp 的 .part 机制承担，重跑自动续传。
func Download(ctx context.Context, bin string, o DownloadOptions) error {
	var prog progress
	var progMu sync.Mutex // stdout/stderr 两个泵 goroutine 并发写
	onLine := func(line string) {
		progMu.Lock()
		defer progMu.Unlock()
		handleLine(&prog, line, o.OnProgress)
	}

	var stderr tailBuffer
	cmd := proc.CommandContext(ctx, bin, DownloadArgs(o)...)
	cmd.Stdout = &lineWriter{onLine: onLine}
	cmd.Stderr = io.MultiWriter(&stderr, &lineWriter{onLine: onLine})
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // 暂停/取消：保持错误链供 manager 判定
		}
		if detail := ytError(stderr.String(), err); detail != "" {
			return fmt.Errorf("yt-dlp 下载失败: %s", detail)
		}
		return fmt.Errorf("yt-dlp 下载失败: %s", err)
	}
	return nil
}

// handleLine 处理 yt-dlp 的一行输出：Destination 行标志进入下一阶段
// （视频→音频），KNP 行按模板解析并回调累计进度，其余行忽略。
func handleLine(p *progress, line string, onProgress func(int64)) {
	if strings.HasPrefix(line, "[download] Destination:") {
		p.nextPhase()
		return
	}
	rest, ok := strings.CutPrefix(line, progressMark)
	if !ok {
		return
	}
	fields := strings.Split(rest, "|")
	if len(fields) == 0 {
		return
	}
	down, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
	if err != nil || down <= 0 { // "NA" 或 0：尚未开始，忽略
		return
	}
	if down <= p.phaseDown {
		return // 阶段内无新增字节：不重复回调
	}
	p.phaseDown = down
	if onProgress != nil {
		onProgress(p.base + p.phaseDown)
	}
}

// progress 跨阶段累计下载字节。阶段内单调取大：续传的 downloaded_bytes
// 天然递增，防御性处理重试导致的回退。
type progress struct {
	base      int64 // 已完成阶段的累计
	phaseDown int64 // 当前阶段已下载
}

func (p *progress) nextPhase() {
	p.base += p.phaseDown
	p.phaseDown = 0
}

func (p *progress) update(down int64) int64 {
	if down > p.phaseDown {
		p.phaseDown = down
	}
	return p.base + p.phaseDown
}

// lineWriter 把进程输出按行（\n 或 \r 分隔）拆给 onLine。
type lineWriter struct {
	buf    []byte
	onLine func(string)
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			w.onLine(line)
		}
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 64<<10 { // 超长无换行：丢弃（进度行不会这么长）
		w.buf = nil
	}
	return len(b), nil
}

// tailBuffer 只保留尾部的缓冲：出错时 stderr 尾部即最近的原因。
type tailBuffer struct {
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.buf = append(t.buf, b...)
	if len(t.buf) > 8<<10 {
		t.buf = t.buf[len(t.buf)-8<<10:]
	}
	return len(b), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }

// isAudioTarget 报告格式是否为纯音频容器（决定用 -x 提取还是合流）。
func isAudioTarget(f string) bool {
	switch f {
	case "m4a", "mp3", "opus", "flac", "wav":
		return true
	}
	return false
}
