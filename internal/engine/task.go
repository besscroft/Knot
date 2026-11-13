// Package engine 实现下载任务模型、队列管理与 HTTP 下载器。
package engine

import (
	"path/filepath"
	"strings"
	"time"
)

// Status 是任务的生命周期状态。
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusPaused  Status = "paused"
	StatusDone    Status = "done"
	StatusError   Status = "error"
)

// Task 是一个下载任务，其 JSON 形态即前端看到的数据。
type Task struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Name       string `json:"name"` // 文件名
	Dir        string `json:"dir"`  // 保存目录
	Status     Status `json:"status"`
	Kind       string `json:"kind,omitempty"` // 空 = http 直链；hls = m3u8 流
	Total      int64  `json:"total"`          // 总字节数，未知为 -1
	Downloaded int64  `json:"downloaded"`
	SegDone    int64  `json:"segDone,omitempty"`  // HLS 已完成分片数
	SegTotal   int64  `json:"segTotal,omitempty"` // HLS 分片总数
	Speed      int64  `json:"speed"`              // 字节/秒，由 manager 周期计算
	Error      string `json:"error,omitempty"`
	Threads    int    `json:"threads,omitempty"` // 分片/并发数，0 表示用默认
	Format     string `json:"format,omitempty"`  // 目标格式（mp4/mkv/mov/webm/m4a/mp3/opus/flac/wav），空 = 原样
	// 浏览上下文：从应用内浏览器创建的任务携带，规避站点防盗链。
	Referer   string  `json:"referer,omitempty"`
	Cookie    string  `json:"cookie,omitempty"` // 不在 UI 展示
	UserAgent string  `json:"userAgent,omitempty"`
	Duration  float64 `json:"duration,omitempty"` // 媒体时长（秒），完成后由 ffprobe 探测，0 = 未知
	// DASH 分轨任务（kind=dash）：AudioURL 为第二路流直链（纯音频任务为空），
	// StreamSize/AudioSize 是两路流各自的字节数，用于进度加权与总大小展示。
	AudioURL    string     `json:"audioUrl,omitempty"`
	StreamSize  int64      `json:"streamSize,omitempty"`
	AudioSize   int64      `json:"audioSize,omitempty"`
	// yt-dlp 任务（kind=yt）：下载全权交给 yt-dlp 子进程，Quality 是画质
	// 描述（展示用），Selector 是创建时锁定的 -f 格式选择表达式。
	Quality  string `json:"quality,omitempty"`
	Selector string `json:"selector,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// Path 返回目标文件的完整路径。
func (t Task) Path() string { return filepath.Join(t.Dir, t.Name) }

// MediaKind 按文件扩展名分类媒体：video / audio / image / other。
func MediaKind(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "mp4", "mkv", "mov", "webm", "m4v", "avi", "flv", "ts", "mpg", "mpeg", "wmv":
		return "video"
	case "mp3", "m4a", "aac", "flac", "wav", "ogg", "opus", "wma":
		return "audio"
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "avif":
		return "image"
	}
	return "other"
}

// ShardDetail 是一个下载分片的实时快照（http 多线程任务专用；
// 其 JSON 形态即前端详情框看到的数据）。
type ShardDetail struct {
	Index    int   `json:"index"`
	Start    int64 `json:"start"`
	End      int64 `json:"end"` // 闭区间；-1 = 未定长单流
	Written  int64 `json:"written"`
	Active   bool  `json:"active"`   // 正在有连接传输
	Retrying int   `json:"retrying"` // 重试轮次，0 = 首次尝试
}

// Size 返回分片总字节数；未定长返回 -1。
func (s ShardDetail) Size() int64 {
	if s.End < 0 {
		return -1
	}
	return s.End - s.Start + 1
}
