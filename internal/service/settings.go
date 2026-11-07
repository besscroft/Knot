// Package service 把下载引擎暴露为 mygo 绑定服务，供前端通过生成的
// TypeScript 客户端（frontend/src/mygo.ts）调用。
package service

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/besscroft/knot/internal/engine"
)

// Settings 是应用设置，持久化在 userData/settings.json。
type Settings struct {
	DownloadDir   string   `json:"downloadDir"`
	Threads       int      `json:"threads"`       // 单任务分片数
	MaxConcurrent int      `json:"maxConcurrent"` // 同时进行的任务数
	Proxy         string   `json:"proxy"`         // 下载代理，空 = 跟随系统/环境
	VideoFormat   string   `json:"videoFormat"`   // 视频默认格式：mp4/mkv/mov/webm，空 = mp4
	AudioFormat   string   `json:"audioFormat"`   // 音频默认格式：m4a/mp3/opus/flac/wav，空 = m4a
	FfmpegPath    string   `json:"ffmpegPath"`    // 手动指定 FFmpeg 路径，空 = 自动查找/下载
	SniffTypes    []string `json:"sniffTypes"`    // 浏览嗅探允许的资源类型，全量见 AllSniffTypes
}

// AllSniffTypes 是嗅探资源类型的全量枚举，与 preload 脚本/嗅探面板的
// type 值一致（info 聚合提示是合成条目，不可配置）。
var AllSniffTypes = []string{"hls", "video", "audio", "image", "archive", "document", "link", "other"}

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
	if !engine.SupportedFormats[strings.ToLower(v.VideoFormat)] && v.VideoFormat != "" {
		v.VideoFormat = ""
	}
	if !engine.SupportedFormats[strings.ToLower(v.AudioFormat)] && v.AudioFormat != "" {
		v.AudioFormat = ""
	}
	v.VideoFormat = strings.ToLower(v.VideoFormat)
	v.AudioFormat = strings.ToLower(v.AudioFormat)

	// 嗅探类型：剔除非法值并去重；空（含旧配置缺字段）= 全部。
	allowed := make(map[string]bool, len(AllSniffTypes))
	for _, t := range AllSniffTypes {
		allowed[t] = true
	}
	types := make([]string, 0, len(AllSniffTypes))
	seen := make(map[string]bool, len(v.SniffTypes))
	for _, t := range v.SniffTypes {
		if allowed[t] && !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		types = append(types, AllSniffTypes...)
	}
	v.SniffTypes = types
}

// sniffTypeSet 返回嗅探类型白名单集合。
func sniffTypeSet(v Settings) map[string]bool {
	m := make(map[string]bool, len(v.SniffTypes))
	for _, t := range v.SniffTypes {
		m[t] = true
	}
	return m
}
