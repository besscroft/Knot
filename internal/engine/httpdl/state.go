package httpdl

import (
	"encoding/json"
	"os"
)

// State 是持久化到磁盘的断点续传状态，存放在目标文件旁的 <文件名>.knot 里。
type State struct {
	URL      string    `json:"url"`
	Size     int64     `json:"size"` // -1 表示未知大小（单流模式）
	ETag     string    `json:"etag,omitempty"`
	Segments []Segment `json:"segments"`
}

// Segment 是一个分片的区间与已写字节数。
type Segment struct {
	Start   int64 `json:"start"`
	End     int64 `json:"end"` // 闭区间；-1 表示未定长的单流下载
	Written int64 `json:"written"`
	// 实时状态（不入盘，仅经 downloader 的快照暴露给上层）：
	Active   bool `json:"-"` // 正在有连接传输
	Retrying int  `json:"-"` // 当前重试轮次，0 = 首次尝试
}

// Remaining 返回该分片剩余字节数；未定长时返回 -1。
func (s Segment) Remaining() int64 {
	if s.End < 0 {
		return -1
	}
	return s.End - s.Start + 1 - s.Written
}

// Complete 报告该分片是否已下载完。
func (s Segment) Complete() bool { return s.Remaining() == 0 }

// SidecarPath 返回目标文件对应的 sidecar 路径。
func SidecarPath(target string) string { return target + ".knot" }

// LoadState 读取 sidecar；不存在时返回 (nil, nil)。
func LoadState(target string) (*State, error) {
	data, err := os.ReadFile(SidecarPath(target))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// SaveState 原子地写入 sidecar（先写临时文件再 rename）。
func SaveState(target string, st *State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	p := SidecarPath(target)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// RemoveState 删除 sidecar 及其残留的临时文件（不存在则忽略）。
func RemoveState(target string) {
	_ = os.Remove(SidecarPath(target))
	_ = os.Remove(SidecarPath(target) + ".tmp")
}
