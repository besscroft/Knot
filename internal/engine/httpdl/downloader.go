package httpdl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	// DefaultThreads 是默认分片数。
	DefaultThreads = 8
	// MinSegmentSize 之下的文件不分片。
	MinSegmentSize = 4 << 20 // 4 MiB
	userAgent      = "Mozilla/5.0 (KHTML, like Gecko) Knot/0.1"
	// progressInterval 是 OnProgress 的回调间隔。
	progressInterval = 200 * time.Millisecond
	// saveInterval 是 sidecar 的持久化间隔。
	saveInterval = 2 * time.Second
	// maxBodyBuffer 是单个分片的写盘缓冲。
	maxBodyBuffer = 64 << 10 // 64 KiB
)

// Options 控制一次下载。
type Options struct {
	// Threads 是分片数，<=1 或服务器不支持 Range 时走单流。
	Threads int
	// Client 为 nil 时使用默认客户端。
	Client *http.Client
	// Headers 是随请求携带的上下文头（Referer/Cookie/UA），可为 nil。
	Headers *Headers
	// OnProgress 约每 progressInterval 回调一次 (downloaded, total)；可为 nil。
	OnProgress func(downloaded, total int64)
}

// Download 把 rawURL 下载到 path。
//
// 若 path 旁存在 sidecar 且与服务器状态吻合则续传；成功后删除 sidecar。
// ctx 取消时保存状态并返回 ctx.Err()，之后可再次调用以续传。
func Download(ctx context.Context, rawURL, path string, opt Options) error {
	client := opt.Client
	if client == nil {
		client = Client()
	}
	threads := opt.Threads
	if threads <= 0 {
		threads = DefaultThreads
	}

	probe, err := probe(ctx, client, rawURL, opt.Headers)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("无法创建文件: %w", err)
	}
	defer f.Close()

	d := &downloader{
		client:  client,
		probe:   probe,
		file:    f,
		threads: threads,
		headers: opt.Headers,
	}
	return d.run(ctx, opt)
}

type downloader struct {
	client  *http.Client
	probe   *ProbeResult
	file    *os.File
	threads int
	headers *Headers

	mu         sync.Mutex // 保护 state 与其 Segment.Written
	state      *State
	dirty      bool
	downloaded atomic.Int64 // 已下载字节（所有分片之和，含历史续传）
}

func (d *downloader) run(ctx context.Context, opt Options) error {
	if err := d.prepareState(); err != nil {
		return err
	}

	// 进度回调与 sidecar 持久化的后台循环。
	done := make(chan struct{})
	var loopWG sync.WaitGroup
	loopWG.Add(2)
	go d.progressLoop(ctx, opt, done, &loopWG)
	go d.saveLoop(ctx, done, &loopWG)
	defer func() {
		close(done)
		loopWG.Wait()
	}()

	g, gctx := errgroup.WithContext(ctx)
	for i := range d.state.Segments {
		seg := &d.state.Segments[i]
		if seg.Complete() {
			d.downloaded.Add(seg.Written)
			continue
		}
		g.Go(func() error { return d.fetchSegment(gctx, seg) })
	}
	err := g.Wait()
	if err != nil {
		d.saveNow() // 保留断点
		return err
	}
	// 成功：裁掉预分配中多余的字节并清理 sidecar。
	if d.state.Size >= 0 {
		_ = d.file.Truncate(d.state.Size)
	}
	RemoveState(d.file.Name())
	return nil
}

// prepareState 决定分片方案：能续传则复用 sidecar，否则新建。
func (d *downloader) prepareState() error {
	size := d.probe.Size
	p := d.file.Name()

	old, _ := LoadState(p)
	if old != nil && !d.resumable(old) {
		old = nil
	}
	if old != nil {
		d.state = old
		return nil
	}

	st := &State{URL: d.probe.URL, Size: size, ETag: d.probe.ETag}
	switch {
	case size > MinSegmentSize && d.probe.AcceptRanges && d.threads > 1:
		st.Segments = splitRanges(size, d.threads)
		_ = d.file.Truncate(size) // 预分配，段间并发 WriteAt
	default:
		// 单流：未知大小用 End=-1 表示未定长。
		end := int64(-1)
		if size >= 0 {
			end = size - 1
		}
		info, err := d.file.Stat()
		if err == nil && info.Size() > 0 && size >= 0 && info.Size() <= size && d.probe.AcceptRanges {
			// 已有内容可按 Range 续传。
			st.Segments = []Segment{{Start: 0, End: end, Written: info.Size()}}
		} else {
			st.Segments = []Segment{{Start: 0, End: end}}
			if size >= 0 {
				_ = d.file.Truncate(0)
			}
		}
	}
	d.state = st
	d.dirty = true
	return nil
}

// resumable 判断旧 sidecar 是否与本次探测吻合。
func (d *downloader) resumable(old *State) bool {
	if old.Size >= 0 && d.probe.Size >= 0 && old.Size != d.probe.Size {
		return false
	}
	if old.ETag != "" && d.probe.ETag != "" && old.ETag != d.probe.ETag {
		return false
	}
	if len(old.Segments) == 0 {
		return false
	}
	// 已有文件内容不得短于断点记录，否则区间会写穿。
	info, err := d.file.Stat()
	if err != nil {
		return false
	}
	var end int64
	for _, s := range old.Segments {
		if s.End > end {
			end = s.End
		}
	}
	return info.Size() >= end+1
}

// splitRanges 把 [0, size) 均分为 n 个闭区间分片。
func splitRanges(size int64, n int) []Segment {
	if int64(n) > size {
		n = int(size)
	}
	chunk := size / int64(n)
	segs := make([]Segment, 0, n)
	var start int64
	for i := 0; i < n; i++ {
		end := start + chunk - 1
		if i == n-1 {
			end = size - 1 // 最后一段吃掉余数
		}
		segs = append(segs, Segment{Start: start, End: end})
		start = end + 1
	}
	return segs
}

// fetchSegment 带退避地反复尝试，直到分片完成、发生致命错误或 ctx 取消。
func (d *downloader) fetchSegment(ctx context.Context, seg *Segment) error {
	const maxAttempts = 12
	backoff := 500 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := d.trySegment(ctx, seg)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if fatalStatus(err) {
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
	return fmt.Errorf("重试 %d 次后仍失败: %w", maxAttempts, lastErr)
}

// trySegment 发起一次 Range 请求，把响应流写进分片。
func (d *downloader) trySegment(ctx context.Context, seg *Segment) error {
	lo := seg.Start + seg.Written
	if seg.End >= 0 && lo > seg.End {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.probe.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	d.headers.Apply(req)
	if lo > 0 || seg.End >= 0 {
		rng := fmt.Sprintf("bytes=%d-", lo)
		if seg.End >= 0 {
			rng = fmt.Sprintf("bytes=%d-%d", lo, seg.End)
		}
		req.Header.Set("Range", rng)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusPartialContent:
		// 期望内
	case resp.StatusCode == http.StatusOK && (lo == 0 || seg.End < 0):
		// 服务器忽略 Range、从头返回：仅单流首段可用，其余情况是致命不匹配
		if seg.Start+seg.Written != 0 {
			return fmt.Errorf("服务器不支持断点续传（返回 %s）", resp.Status)
		}
		d.rewindSegment(seg)
	default:
		return &statusError{Code: resp.StatusCode}
	}

	buf := make([]byte, maxBodyBuffer)
	offset := lo
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if seg.End >= 0 && offset+int64(n) > seg.End+1 {
				n = int(seg.End + 1 - offset) // 服务器越界时截断
			}
			if _, werr := d.file.WriteAt(buf[:n], offset); werr != nil {
				return werr
			}
			offset += int64(n)
			d.advance(seg, int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
		if seg.End >= 0 && offset > seg.End {
			break
		}
	}
	if seg.End >= 0 && offset <= seg.End {
		return io.ErrUnexpectedEOF // 连接提前断开，交给重试
	}
	return nil
}

// rewindSegment 处理"服务器忽略 Range 从头返回"：丢弃旧内容重写本段。
func (d *downloader) rewindSegment(seg *Segment) {
	d.mu.Lock()
	d.downloaded.Add(-seg.Written)
	seg.Written = 0
	d.dirty = true
	d.mu.Unlock()
}

// advance 记录一次写盘，并标记 sidecar 待保存。
func (d *downloader) advance(seg *Segment, n int64) {
	d.mu.Lock()
	seg.Written += n
	d.dirty = true
	d.mu.Unlock()
	d.downloaded.Add(n)
}

func (d *downloader) progressLoop(ctx context.Context, opt Options, done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	t := time.NewTicker(progressInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-t.C:
			if opt.OnProgress != nil {
				opt.OnProgress(d.downloaded.Load(), d.state.Size)
			}
		}
	}
}

func (d *downloader) saveLoop(ctx context.Context, done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	t := time.NewTicker(saveInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			d.saveNow()
			return
		case <-done:
			return
		case <-t.C:
			d.saveNow()
		}
	}
}

func (d *downloader) saveNow() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.dirty {
		return
	}
	if err := SaveState(d.file.Name(), d.state); err == nil {
		d.dirty = false
	}
}

// statusError 表示不可重试语义的 HTTP 状态码。
type statusError struct{ Code int }

func (e *statusError) Error() string { return fmt.Sprintf("服务器返回 HTTP %d", e.Code) }

// fatalStatus 报告该错误是否不值得重试（如 403/404）。
func fatalStatus(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		switch se.Code {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusGone, http.StatusRequestedRangeNotSatisfiable:
			return true
		}
		return false
	}
	return false
}
