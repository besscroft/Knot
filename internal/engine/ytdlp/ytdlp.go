// Package ytdlp 封装 yt-dlp 外部二进制：查找/自动下载（GitHub Releases
// 单文件构建）、-J 解析与画质归并、下载执行与进度解析。YouTube 的反爬
// 与格式调整频繁，解析和下载都交给持续维护的 yt-dlp，而不是抽取直链
// 自行下载（直链带签名时效与节流，抽取后很快失效）。
package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/besscroft/knot/internal/engine/httpdl"
)

var (
	ytdlpPath     string // 设置里手动指定的 yt-dlp 路径
	ytdlpCacheDir string // 自动下载的存放目录（PathCache/ytdlp）
	probeCache    *string
	probeVersion  string
)

// SetOptions 应用 yt-dlp 配置（设置变更或启动时调用）。
func SetOptions(path, cacheDir string) {
	ytdlpPath = strings.TrimSpace(path)
	ytdlpCacheDir = cacheDir
	probeCache = nil
	probeVersion = ""
}

// cachedBin 自动下载产物的位置。
func cachedBin() string {
	if ytdlpCacheDir == "" {
		return ""
	}
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name = "yt-dlp.exe"
	}
	return filepath.Join(ytdlpCacheDir, name)
}

// version 运行 --version 校验路径，成功时返回版本串并缓存。
func version(ctx context.Context, given string) (string, bool) {
	if given == "" {
		return "", false
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, given, "--version").Output()
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", false
	}
	probeCache = &given
	probeVersion = v
	return v, true
}

// Find 依次查找：缓存 → 自动下载缓存 → 设置指定路径 → PATH。
// 返回可用路径（找不到为空串）。
func Find(ctx context.Context) string {
	if probeCache != nil {
		if _, ok := version(ctx, *probeCache); ok {
			return *probeCache
		}
	}
	if p := cachedBin(); p != "" {
		if _, ok := version(ctx, p); ok {
			return p
		}
	}
	if ytdlpPath != "" {
		if _, ok := version(ctx, ytdlpPath); ok {
			return ytdlpPath
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		if _, ok := version(ctx, p); ok {
			return p
		}
	}
	return ""
}

// Version 返回当前可用 yt-dlp 的版本串（设置页展示用），不可用为空串。
func Version(ctx context.Context) string {
	Find(ctx)
	return probeVersion
}

// Ensure 返回可用的 yt-dlp 路径；本机没有且 allowDownload 时自动下载
// 对应平台的单文件构建并校验。
func Ensure(ctx context.Context, allowDownload bool) (string, error) {
	if p := Find(ctx); p != "" {
		return p, nil
	}
	if !allowDownload {
		return "", errors.New("未找到 yt-dlp")
	}
	if err := download(ctx); err != nil {
		return "", err
	}
	if p := Find(ctx); p != "" {
		log.Println("ytdlp: 自动下载完成:", p)
		return p, nil
	}
	return "", errors.New("yt-dlp 下载后校验失败")
}

// buildURL 当前平台的下载源（GitHub Releases 单文件二进制；Windows ARM64
// 官方无单独构建，走 x64 版本经系统模拟运行）。
func buildURL() (string, bool) {
	switch {
	case runtime.GOOS == "windows":
		return "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe", true
	case runtime.GOOS == "darwin":
		return "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_macos", true
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux", true
	}
	return "", false
}

// download 下载单文件二进制到缓存目录（httpdl.Client 走设置的代理，便于
// GitHub 直连受限的环境），以 --version 校验可用。
func download(ctx context.Context) error {
	buildURL, ok := buildURL()
	if !ok {
		return fmt.Errorf("暂不支持自动下载 %s/%s 的 yt-dlp，请在设置中手动指定路径", runtime.GOOS, runtime.GOARCH)
	}
	if ytdlpCacheDir == "" {
		return errors.New("缓存目录未就绪")
	}
	if err := os.MkdirAll(ytdlpCacheDir, 0o755); err != nil {
		return err
	}

	log.Println("ytdlp: 开始自动下载:", buildURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL, nil)
	if err != nil {
		return err
	}
	resp, err := httpdl.Client().Do(req)
	if err != nil {
		return fmt.Errorf("下载 yt-dlp 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 yt-dlp 失败: 服务器返回 %s", resp.Status)
	}

	dst := cachedBin()
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	size, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if size < 10<<20 {
		os.Remove(tmp)
		return fmt.Errorf("下载的 yt-dlp 过小（%d 字节），已放弃", size)
	}
	// 覆盖旧版本：Windows 上 Rename 不能覆盖已存在文件，先移除。
	os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	log.Printf("ytdlp: 下载完成（%.1f MB）", float64(size)/(1<<20))
	return nil
}
