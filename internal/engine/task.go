// Package engine 实现下载任务模型、队列管理与 HTTP 下载器。
package engine

import (
	"path/filepath"
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
	// 浏览上下文：从应用内浏览器创建的任务携带，规避站点防盗链。
	Referer     string     `json:"referer,omitempty"`
	Cookie      string     `json:"cookie,omitempty"` // 不在 UI 展示
	UserAgent   string     `json:"userAgent,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// Path 返回目标文件的完整路径。
func (t Task) Path() string { return filepath.Join(t.Dir, t.Name) }
