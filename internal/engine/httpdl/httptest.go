package httpdl

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// rangeServer 返回一个支持 Range 的测试服务器，内容为 data。
func rangeServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
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
	t.Cleanup(srv.Close)
	return srv
}

// randomBytes 生成 n 字节的伪随机内容。
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}
