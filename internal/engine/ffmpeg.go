// FFmpeg 支持：查找本机 FFmpeg，缺失时自动下载（参考 Downbit：首次使用
// 自动获取并校验，之后转换均为流拷贝，速度很快）。
package engine

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/besscroft/knot/internal/proc"
)

var (
	ffmpegPath     string // 设置里手动指定的 FFmpeg 路径
	ffmpegCacheDir string // 自动下载的存放目录（PathCache/ffmpeg）
)

// SetFfmpegOptions 应用 FFmpeg 配置（设置变更或启动时调用）。
func SetFfmpegOptions(path, cacheDir string) {
	ffmpegPath = strings.TrimSpace(path)
	ffmpegCacheDir = cacheDir
	probeFfmpegCache = nil
}

// cachedFfmpeg 是自动下载产物的位置。
func cachedFfmpeg() string {
	if ffmpegCacheDir == "" {
		return ""
	}
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	return filepath.Join(ffmpegCacheDir, name)
}

var probeFfmpegCache *string

// works 报告 given 路径是否为可用的 ffmpeg。
func works(ctx context.Context, given string) bool {
	if given == "" {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := proc.CommandContext(c, given, "-version").Output()
	if err != nil {
		return false
	}
	probeFfmpegCache = &given
	return strings.Contains(string(out), "ffmpeg")
}

// FindFfmpeg 依次查找：设置指定路径 → 自动下载缓存 → PATH。
func FindFfmpeg(ctx context.Context) string {
	if probeFfmpegCache != nil && works(ctx, *probeFfmpegCache) {
		return *probeFfmpegCache
	}
	if p := cachedFfmpeg(); p != "" && works(ctx, p) {
		return p
	}
	if ffmpegPath != "" && works(ctx, ffmpegPath) {
		return ffmpegPath
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil && works(ctx, p) {
		return p
	}
	return ""
}

// EnsureFfmpeg 返回可用的 ffmpeg 路径；本机没有且 allowDownload 时
// 自动下载对应平台的构建并校验。
func EnsureFfmpeg(ctx context.Context, allowDownload bool) (string, error) {
	if p := FindFfmpeg(ctx); p != "" {
		return p, nil
	}
	if !allowDownload {
		return "", fmt.Errorf("未找到 FFmpeg")
	}
	if err := downloadFfmpeg(ctx); err != nil {
		return "", err
	}
	if p := FindFfmpeg(ctx); p != "" {
		log.Println("ffmpeg: 自动下载完成:", p)
		return p, nil
	}
	return "", fmt.Errorf("FFmpeg 下载后校验失败")
}

// FindFfprobe 查找 ffprobe（时长探测用）：工作 ffmpeg 的同目录 → PATH。
// 官方构建里两者总是同目录分发。
func FindFfprobe(ctx context.Context) string {
	want := "ffprobe"
	if runtime.GOOS == "windows" {
		want = "ffprobe.exe"
	}
	var candidates []string
	if p := FindFfmpeg(ctx); p != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(p), want))
	}
	if p, err := exec.LookPath(want); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := proc.CommandContext(c, p, "-version").Output()
		cancel()
		if err == nil && strings.Contains(string(out), "ffprobe") {
			return p
		}
	}
	return ""
}

// ProbeDuration 返回媒体时长（秒）；无法确定时返回 0。
func ProbeDuration(ctx context.Context, path string) float64 {
	ffp := FindFfprobe(ctx)
	if ffp == "" || path == "" {
		return 0
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := proc.CommandContext(c, ffp,
		"-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path,
	).Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if d < 0 {
		return 0
	}
	return d
}

// ffmpegBuild 是当前平台的下载源（zip 包，内置校验：解压后运行 -version）。
func ffmpegBuild() (url string, ok bool) {
	switch {
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip", true
	case runtime.GOOS == "windows" && runtime.GOARCH == "arm64":
		return "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-winarm64-gpl.zip", true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "https://evermeet.cx/ffmpeg/getrelease/zip", true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return "https://evermeet.cx/ffmpeg/getrelease/zip", true
	}
	return "", false
}

// downloadFfmpeg 下载并解压 ffmpeg 到缓存目录（zip 用标准库解压，
// 只抽取 ffmpeg 可执行文件本身，然后以 `-version` 运行校验）。
func downloadFfmpeg(ctx context.Context) error {
	buildURL, ok := ffmpegBuild()
	if !ok {
		return fmt.Errorf("暂不支持自动下载 %s/%s 的 FFmpeg，请在设置中手动指定路径", runtime.GOOS, runtime.GOARCH)
	}
	if ffmpegCacheDir == "" {
		return fmt.Errorf("缓存目录未就绪")
	}
	if err := os.MkdirAll(ffmpegCacheDir, 0o755); err != nil {
		return err
	}

	log.Println("ffmpeg: 开始自动下载:", buildURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL, nil)
	if err != nil {
		return err
	}
	resp, err := httpdl.Client().Do(req)
	if err != nil {
		return fmt.Errorf("下载 FFmpeg 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 FFmpeg 失败: 服务器返回 %s", resp.Status)
	}

	zipPath := filepath.Join(ffmpegCacheDir, "ffmpeg-download.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	size, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		return err
	}
	if size < 10<<20 {
		os.Remove(zipPath)
		return fmt.Errorf("下载的 FFmpeg 包过小（%d 字节），已放弃", size)
	}
	log.Printf("ffmpeg: 下载完成（%.1f MB），解压中", float64(size)/(1<<20))

	if err := extractFfmpegFromZip(zipPath); err != nil {
		os.Remove(zipPath)
		return err
	}
	os.Remove(zipPath)
	return nil
}

// extractFfmpegFromZip 从 zip 中抽取 ffmpeg 与 ffprobe（各自取最大的
// 同名项；ffprobe 缺失不致命，只是时长探测不可用）。
func extractFfmpegFromZip(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("解压 FFmpeg 失败: %w", err)
	}
	defer zr.Close()

	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if err := extractToolFromZip(zr, tool); err != nil {
			if tool == "ffmpeg" {
				return err
			}
			log.Println("ffmpeg: 未在压缩包中找到 ffprobe，时长探测不可用")
		}
	}
	return nil
}

// extractToolFromZip 抽取 zip 中的单个可执行工具到缓存目录。
func extractToolFromZip(zr *zip.ReadCloser, tool string) error {
	want := tool
	if runtime.GOOS == "windows" {
		want = tool + ".exe"
	}
	var best *zip.File
	for i := range zr.File {
		ze := zr.File[i]
		if ze.FileInfo().IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Base(ze.Name), want) {
			continue
		}
		if best == nil || ze.UncompressedSize64 > best.UncompressedSize64 {
			best = ze
		}
	}
	if best == nil {
		return fmt.Errorf("压缩包中没有找到 %s", want)
	}

	rc, err := best.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	dst := filepath.Join(ffmpegCacheDir, want)
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
