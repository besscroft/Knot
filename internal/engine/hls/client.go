package hls

import (
	"net/http"

	"github.com/besscroft/knot/internal/engine/httpdl"
)

// httpClient 返回共享 HTTP 客户端（代理配置随设置生效）。
func httpClient() *http.Client { return httpdl.Client() }
