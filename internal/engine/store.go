package engine

import (
	"encoding/json"
	"os"
)

// Store 把任务列表持久化为 JSON（userData/tasks.json），写入是原子的。
type Store struct {
	path string
}

// NewStore 使用 path 处的 JSON 文件创建任务存储。
func NewStore(path string) *Store { return &Store{path: path} }

// Load 读取全部任务；文件不存在时返回 (nil, nil)。
func (s *Store) Load() ([]Task, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// Backup 把任务文件改名保留（加载失败时调用：避免随后的空列表写入把
// 可能可抢救的数据覆盖掉）。
func (s *Store) Backup() error {
	if _, err := os.Lstat(s.path); err != nil {
		return err
	}
	return os.Rename(s.path, s.path+".corrupt")
}

// Save 原子地写入全部任务。
func (s *Store) Save(tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
