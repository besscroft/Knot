// Package service 把下载引擎暴露为 mygo 绑定服务，供前端通过生成的
// TypeScript 客户端（frontend/src/mygo.ts）调用。
package service

import (
	"encoding/json"
	"os"
	"sync"
)

// Settings 是应用设置，持久化在 userData/settings.json。
type Settings struct {
	DownloadDir   string `json:"downloadDir"`
	Threads       int    `json:"threads"`       // 单任务分片数
	MaxConcurrent int    `json:"maxConcurrent"` // 同时进行的任务数
	Proxy         string `json:"proxy"`         // 下载代理，空 = 跟随系统/环境
}

// settingsStore 线程安全地读写设置文件。
type settingsStore struct {
	mu   sync.Mutex
	path string
	val  Settings
}

func loadSettings(path string, def Settings) (*settingsStore, error) {
	s := &settingsStore{path: path, val: def}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.val); err != nil {
		return nil, err
	}
	sanitizeSettings(&s.val, def.DownloadDir)
	return s, nil
}

// Get 返回当前设置副本。
func (s *settingsStore) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.val
}

// Set 校验并保存设置。
func (s *settingsStore) Set(v Settings) error {
	sanitizeSettings(&v, s.val.DownloadDir)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.mu.Lock()
	s.val = v
	s.mu.Unlock()
	return nil
}

// sanitizeSettings 补齐非法值。
func sanitizeSettings(v *Settings, defDir string) {
	if v.DownloadDir == "" {
		v.DownloadDir = defDir
	}
	if v.Threads < 1 {
		v.Threads = 8
	}
	if v.Threads > 64 {
		v.Threads = 64
	}
	if v.MaxConcurrent < 1 {
		v.MaxConcurrent = 3
	}
	if v.MaxConcurrent > 10 {
		v.MaxConcurrent = 10
	}
}
