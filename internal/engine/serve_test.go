package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// serveStatic 返回一个支持 Range 的静态内容服务器（engine 包测试用）。
func serveStatic(t *testing.T, data []byte) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"engine-test"`)
		rng := r.Header.Get("Range")
		if rng == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		spec := strings.TrimPrefix(rng, "bytes=")
		loStr, hiStr, _ := strings.Cut(spec, "-")
		lo, _ := strconv.ParseInt(loStr, 10, 64)
		hi := int64(len(data)) - 1
		if hiStr != "" {
			hi, _ = strconv.ParseInt(hiStr, 10, 64)
		}
		if lo >= int64(len(data)) || hi < lo {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", lo, hi, len(data)))
		w.Header().Set("Content-Length", strconv.FormatInt(hi-lo+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[lo : hi+1])
	}))
	return srv.URL, srv.Close
}
