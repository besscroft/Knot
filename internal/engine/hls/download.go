package hls

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/besscroft/knot/internal/engine/httpdl"
	"golang.org/x/sync/errgroup"
)

// Options 控制一次 HLS 下载。
type Options struct {
	// Threads 是并发下载分片数，<1 时取 1。
	Threads int
	// Client 为 nil 时使用 httpdl.Client()（共享、代理已配置）。
	Client *http.Client
	// Headers 是随请求携带的上下文头（Referer/Cookie/UA），可为 nil。
	Headers *httpdl.Headers
	// OnProgress 约每 200ms 回调一次（已下载字节, 已完成分片, 分片总数）。
	OnProgress func(downloaded int64, segDone, segTotal int)
}

// Download 执行完整的 HLS 下载：
//
//	解析播放列表 → 分片并发下载到 workDir → 拼接 all.ts → 转封装 finalPath
//
// finalPath 以 .mp4 结尾；转封装失败时把拼接结果保留为同名 .ts 并返回错误。
// 分片文件按序号落在 workDir，已存在的分片直接复用（断点续传）；
// ctx 取消时保留 workDir，可再次调用续传。
func Download(ctx context.Context, mediaURL, workDir, finalPath string, opt Options) error {
	allTS, err := downloadSegments(ctx, mediaURL, workDir, opt)
	if err != nil {
		return err
	}
	if err := Remux(allTS, finalPath); err != nil {
		tsFallback := strings.TrimSuffix(finalPath, ".mp4") + ".ts"
		if mvErr := os.Rename(allTS, tsFallback); mvErr == nil {
			return fmt.Errorf("转封装 MP4 失败（%v），已保留 TS 文件: %s", err, tsFallback)
		}
		return fmt.Errorf("转封装 MP4 失败: %w", err)
	}
	// 成功：清理工作目录。
	_ = os.RemoveAll(workDir)
	return nil
}

// downloadSegments 下载全部分片并拼接为单个 TS（workDir/all.ts）。
func downloadSegments(ctx context.Context, mediaURL, workDir string, opt Options) (string, error) {
	client := opt.Client
	if client == nil {
		client = httpClient()
	}
	threads := opt.Threads
	if threads < 1 {
		threads = 1
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建工作目录: %w", err)
	}

	media, err := Parse(ctx, client, mediaURL, opt.Headers)
	if err != nil {
		return "", err
	}

	// 分片文件路径与初始进度（已存在的分片视为完成，计入字节）。
	// 缓存首字节不是 TS 同步字说明内容损坏（如旧版本曾把未解密分片落盘），
	// 删除以便重新下载。
	paths := make([]string, len(media.Segments))
	var doneBytes int64
	for i, seg := range media.Segments {
		p := filepath.Join(workDir, fmt.Sprintf("seg_%06d.ts", seg.Index))
		paths[i] = p
		info, err := os.Stat(p)
		switch {
		case err != nil || info.Size() == 0:
			// 不存在或空文件：待下载
		case cachedSegmentValid(p):
			doneBytes += info.Size()
		default:
			_ = os.Remove(p)
		}
	}

	prog := &progresser{
		total: len(paths),
		stop:  make(chan struct{}),
	}
	prog.downled.Store(doneBytes)
	prog.segDone.Store(countDoneSegments(paths))
	go prog.loop(ctx, opt.OnProgress)
	defer close(prog.stop)

	// 待下载分片入队。
	idxCh := make(chan int, len(paths))
	for i := range paths {
		if info, err := os.Stat(paths[i]); err == nil && info.Size() > 0 {
			continue
		}
		idxCh <- i
	}
	close(idxCh)

	var keys keyStore
	g, gctx := errgroup.WithContext(ctx)
	for w := 0; w < threads; w++ {
		g.Go(func() error {
			for i := range idxCh {
				if err := gctx.Err(); err != nil {
					return err
				}
				if err := downloadSegment(gctx, client, media.Segments[i], paths[i], &keys, opt.Headers); err != nil {
					return err
				}
				if info, statErr := os.Stat(paths[i]); statErr == nil {
					prog.addBytes(info.Size())
				}
				prog.addSeg()
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err() // 暂停/取消：workDir 保留供续传
		}
		return "", err
	}

	// 拼接为单个 TS。
	allTS := filepath.Join(workDir, "all.ts")
	if err := concatSegments(paths, allTS); err != nil {
		return "", fmt.Errorf("拼接分片失败: %w", err)
	}
	return allTS, nil
}

// downloadSegment 下载单个分片（带退避重试），加密流先取密钥并解密。
func downloadSegment(ctx context.Context, client *http.Client, seg Segment, path string, keys *keyStore, headers *httpdl.Headers) error {
	backoff := 500 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := trySegment(ctx, client, seg, path, keys, headers)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
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
	return fmt.Errorf("分片 %d 重试后仍失败: %w", seg.Index, lastErr)
}

func trySegment(ctx context.Context, client *http.Client, seg Segment, path string, keys *keyStore, headers *httpdl.Headers) error {
	body, err := fetchSegmentBody(ctx, client, seg.URL, headers)
	if err != nil {
		return err
	}
	if seg.KeyURI != "" {
		key, err := keys.get(ctx, client, seg.KeyURI, headers)
		if err != nil {
			return err
		}
		iv, err := ivFor(seg)
		if err != nil {
			return err
		}
		body, err = decryptAES128(body, key, iv)
		if err != nil {
			return err
		}
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fetchSegmentBody(ctx context.Context, client *http.Client, rawURL string, headers *httpdl.Headers) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	headers.Apply(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("分片请求返回 %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// concatSegments 按顺序把分片拼接为单个 TS 文件（MPEG-TS 可直接字节拼接）。
func concatSegments(paths []string, out string) error {
	dst, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, p := range paths {
		src, err := os.Open(p)
		if err != nil {
			dst.Close()
			return fmt.Errorf("打开分片 %s: %w", filepath.Base(p), err)
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		if copyErr != nil {
			dst.Close()
			return copyErr
		}
	}
	return dst.Close()
}

func countDoneSegments(paths []string) int64 {
	var n int64
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.Size() > 0 {
			n++
		}
	}
	return n
}

// cachedSegmentValid 检查缓存分片是否像有效 TS（首字节为同步字）。
// 仅作粗检：伪同步字等畸形内容由转封装阶段容错处理。
func cachedSegmentValid(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [1]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	return head[0] == tsSyncByte
}

// keyStore 缓存已取回的密钥。
type keyStore struct {
	mu    sync.Mutex
	cache map[string][]byte
}

func (ks *keyStore) get(ctx context.Context, client *http.Client, uri string, headers *httpdl.Headers) ([]byte, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.cache == nil {
		ks.cache = map[string][]byte{}
	}
	if key, ok := ks.cache[uri]; ok {
		return key, nil
	}
	key, err := fetchKey(ctx, client, uri, headers)
	if err != nil {
		return nil, err
	}
	ks.cache[uri] = key
	return key, nil
}

// progresser 聚合进度并节流回调。
type progresser struct {
	total   int
	downled atomic.Int64
	segDone atomic.Int64
	stop    chan struct{}
}

func (p *progresser) addBytes(n int64) { p.downled.Add(n) }
func (p *progresser) addSeg()          { p.segDone.Add(1) }

func (p *progresser) loop(ctx context.Context, onProgress func(int64, int, int)) {
	if onProgress == nil {
		<-p.stop
		return
	}
	onProgress(p.downled.Load(), int(p.segDone.Load()), p.total) // 立即回报初始进度
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stop:
			return
		case <-t.C:
			onProgress(p.downled.Load(), int(p.segDone.Load()), p.total)
		}
	}
}
