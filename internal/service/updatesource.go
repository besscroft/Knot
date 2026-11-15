// 官网更新源:实现 mygo.UpdateSource(mygo.Updater.SetSource)。
//
// manifest 从官网 Worker 的 /update/ 取(与 mygo.config.ts 的 updates.url
// 同一地址);更新包与 delta 用多连接 Range 分片下载 —— 国内到 Cloudflare
// 免费版的单连接大文件传输抖动严重(实测 1KB/s~1MB/s 波动),多连接各占各的
// 流量配额,实测可稳定在 MB/s 级。分片各自带断点重试,写入有序管道,消费端
// 按序读出,框架的下载进度条照常工作。任何异常退回单连接直读,保证功能永远
// 可用;完整性(size + Ed25519 验签)仍由框架在流上校验。
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"

	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/egoist/mygo"
)

// updateFeedBase 与 mygo.config.ts 的 updates.url 一致。
const updateFeedBase = "https://knot.zzzvoid.com/update"

// 并行分片参数:单片下限 512KiB(太小则连接开销占比过高),整体上限 64MiB
// (分片在内存管道中流转,超过则单连接流式,避免内存膨胀)。
const (
	updateParts       = 8
	updatePartMinSize = 512 << 10
	updateParallelMax = 64 << 20
	updatePartRetries = 3
)

// mygoUpdateSource 实现 mygo.UpdateSource。
type mygoUpdateSource struct {
	client *http.Client
	base   string
}

// NewUpdateSource 返回官网更新源。client 为 nil 时用 httpdl 共享客户端
// (应用内的代理设置已应用);测试可注入 httptest 客户端。
func NewUpdateSource(client *http.Client) mygo.UpdateSource {
	if client == nil {
		client = httpdl.Client()
	}
	return mygoUpdateSource{client: client, base: updateFeedBase}
}

// updateTarget 与 mygo build 的 target 命名一致:darwin 构建为 universal。
func updateTarget() string {
	if runtime.GOOS == "darwin" {
		return "darwin-universal"
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Manifest 返回当前平台的更新 manifest(update-<target>.json)。
func (s mygoUpdateSource) Manifest(ctx context.Context, _ bool) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/update-"+updateTarget()+".json", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mygo-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update manifest: %s", resp.Status)
	}
	return resp.Body, nil
}

// Open 下载 url 指向的更新文件(更新包或 delta)。
func (s mygoUpdateSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	size, ok, err := s.probeSize(ctx, url)
	if err == nil && ok && size > 0 && size <= updateParallelMax {
		return s.openParallel(ctx, url, size)
	}
	return s.openDirect(ctx, url)
}

// probeSize 返回文件大小与是否支持 Range 分片。
func (s mygoUpdateSource) probeSize(ctx context.Context, url string) (int64, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("User-Agent", "mygo-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("probe update file: %s", resp.Status)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		return 0, false, nil
	}
	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil || size <= 0 {
		return 0, false, nil
	}
	return size, true, nil
}

// openDirect 单连接直读(兜底路径,流式不经内存)。
func (s mygoUpdateSource) openDirect(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mygo-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download update: %s", resp.Status)
	}
	return resp.Body, nil
}

// openParallel 并行分片下载,按序读出。每片一个 goroutine 写入自己的管道,
// 消费端按 0..n-1 顺序读;后面的分片在消费前面分片的同时并行预取,框架的
// 进度条按实际流出的字节推进。
func (s mygoUpdateSource) openParallel(ctx context.Context, url string, size int64) (io.ReadCloser, error) {
	parts := updateParts
	if size/int64(parts) < updatePartMinSize {
		parts = int(size / updatePartMinSize)
		if parts < 2 {
			parts = 2
		}
	}
	chunk := (size + int64(parts) - 1) / int64(parts)

	readers := make([]io.ReadCloser, parts)
	for i := range parts {
		start := int64(i) * chunk
		end := min(start+chunk, size) - 1
		pr, pw := io.Pipe()
		readers[i] = pr
		go s.downloadPart(ctx, url, start, end, pw)
	}
	return &multiReadCloser{readers: readers}, nil
}

// downloadPart 把 [start,end] 写入 pw:连接中断时从断点续传该分片
// (Range 让重试对消费端透明),重试耗尽才以错误关闭管道。
func (s mygoUpdateSource) downloadPart(ctx context.Context, url string, start, end int64, pw *io.PipeWriter) {
	defer pw.Close()
	for offset, attempts := start, updatePartRetries; ; attempts-- {
		if attempts == 0 {
			pw.CloseWithError(fmt.Errorf("update part %d-%d: retries exhausted", start, end))
			return
		}
		written, err := s.fetchRange(ctx, url, offset, end, pw)
		if err == nil {
			return
		}
		offset += written
	}
}

// fetchRange 请求 [start,end] 分片并写入 w,返回已写入字节数(供断点重试)。
func (s mygoUpdateSource) fetchRange(ctx context.Context, url string, start, end int64, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "mygo-updater")
	req.Header.Set("Range", "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("range download: %s", resp.Status)
	}
	return io.Copy(w, resp.Body)
}

// multiReadCloser 按序串联多个 ReadCloser,Close 时全部关闭(未消费的分片
// 管道也会被关掉,写端 goroutine 随即退出,不泄漏)。
type multiReadCloser struct {
	readers []io.ReadCloser
	current int
}

func (m *multiReadCloser) Read(p []byte) (int, error) {
	for m.current < len(m.readers) {
		n, err := m.readers[m.current].Read(p)
		if errors.Is(err, io.EOF) {
			_ = m.readers[m.current].Close()
			m.current++
			continue
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.EOF
}

func (m *multiReadCloser) Close() error {
	var firstErr error
	for _, r := range m.readers[m.current:] {
		if err := r.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.current = len(m.readers)
	return firstErr
}
