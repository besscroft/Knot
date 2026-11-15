package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// rangeServer 提供标准 Range 语义的静态文件服务(206/200)。
func rangeServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
		rng := r.Header.Get("Range")
		if rng == "" || r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write(data)
			return
		}
		start, end, ok := parseRange(rng, int64(len(data)))
		if !ok {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(int(end-start+1)))
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
}

func parseRange(value string, size int64) (int64, int64, bool) {
	rest, ok := strings.CutPrefix(value, "bytes=")
	if !ok {
		return 0, 0, false
	}
	startText, endText, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err := strconv.ParseInt(endText, 10, 64)
	if err != nil || end < start || start >= size {
		return 0, 0, false
	}
	return start, min(end, size-1), true
}

// flakyRangeServer 模拟抖动链路:第一个分片请求写几个字节就中断连接,
// 之后所有请求正常 —— 用于验证 downloadPart 的断点续传重试。
func flakyRangeServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	var flakeOnce sync.Once
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		if rng == "" || r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write(data)
			return
		}
		start, end, ok := parseRange(rng, int64(len(data)))
		if !ok {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		flake := false
		flakeOnce.Do(func() { flake = true })
		if flake {
			// 模拟连接中断:写出该 Range 的真实前缀几个字节就断开
			// (真实断链写出的字节是正确前缀,客户端按字节数断点续传)。
			w.Header().Set("Content-Length", "999999")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start : start+7])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Length", strconv.Itoa(int(end-start+1)))
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
}

func TestOpenParallelAssembly(t *testing.T) {
	// 大于 2*512KiB,保证走多分片路径。
	data := make([]byte, (512<<10)*3+123)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	ts := rangeServer(t, data)
	defer ts.Close()

	src := NewUpdateSource(ts.Client())
	rc, err := src.Open(context.Background(), ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("分片拼装结果不一致: got %d bytes, want %d", len(got), len(data))
	}
}

func TestOpenRetriesAfterBrokenPart(t *testing.T) {
	data := make([]byte, (512<<10)*3+123)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	ts := flakyRangeServer(t, data)
	defer ts.Close()

	src := NewUpdateSource(ts.Client())
	rc, err := src.Open(context.Background(), ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("断点重试后拼装结果不一致: got %d bytes, want %d", len(got), len(data))
	}
}

func TestOpenFallbackWhenRangeUnsupported(t *testing.T) {
	// 不回 Accept-Ranges 的服务端:应退回单连接直读,内容仍完整。
	data := []byte("hello knot update")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer ts.Close()

	src := NewUpdateSource(ts.Client())
	rc, err := src.Open(context.Background(), ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("单连接兜底内容不一致: got %q", got)
	}
}

func TestManifestURL(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"version":"9.9.9"}`)
	}))
	defer ts.Close()

	src := mygoUpdateSource{client: ts.Client(), base: ts.URL}
	rc, err := src.Manifest(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatal(err)
	}
	want := "/update-" + updateTarget() + ".json"
	if gotPath != want {
		t.Fatalf("manifest 路径 = %q, want %q", gotPath, want)
	}
}

func TestOpenErrorPropagatesAfterRetries(t *testing.T) {
	// 一直 500:要么 Open 阶段就报错(探测失败退回直读,直读立刻 500),
	// 要么流上读到错误 —— 两者都算正确,唯独不能静默给出完整流。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	src := NewUpdateSource(ts.Client())
	rc, err := src.Open(context.Background(), ts.URL)
	if err != nil {
		return
	}
	defer rc.Close()
	if _, err = io.ReadAll(rc); err == nil {
		t.Fatal("期望读到错误,却得到了完整流")
	}
}
