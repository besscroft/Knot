package httpdl

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/netproxy"
)

// shared 是全部 HTTP 探测与下载共用的客户端；SetProxy 时整体换新。
var shared atomic.Pointer[http.Client]

// proxyRaw 记录最近一次显式设置的代理（空 = 跟随系统/环境），供外部
// 工具（yt-dlp 等子进程）对齐下载器的网络出口。
var proxyRaw atomic.Pointer[string]

func init() {
	shared.Store(newClient(nil))
}

// newClient 构建一个用 proxyFn 决定代理的客户端；proxyFn 为 nil 时跟随
// 系统与环境代理（与 webview 行为一致）。
func newClient(proxyFn func(*http.Request) (*url.URL, error)) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 proxyFn,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// Client 返回共享客户端。长下载由请求 ctx 控制，不设整体超时。
func Client() *http.Client { return shared.Load() }

// SetProxy 切换共享客户端的代理。raw 为空表示跟随系统/环境代理；
// 支持 http://、https://、socks5://，可带 user:pass@。校验失败返回错误且不生效。
func SetProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	var proxyFn func(*http.Request) (*url.URL, error)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("代理地址无效: %w", err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		case "":
			return fmt.Errorf("代理地址缺少协议（http://、https:// 或 socks5://）")
		default:
			return fmt.Errorf("不支持的代理协议 %q（支持 http/https/socks5）", u.Scheme)
		}
		proxyFn = http.ProxyURL(u)
	} else {
		proxyFn = netproxy.Proxy
	}

	old := Client()
	shared.Store(newClient(proxyFn))
	proxyRaw.Store(&raw)
	if old != nil && old.Transport != nil {
		type idleCloser interface{ CloseIdleConnections() }
		if c, ok := old.Transport.(idleCloser); ok {
			c.CloseIdleConnections()
		}
		old.CloseIdleConnections()
	}
	return nil
}

// ProxyTarget 返回外部工具（yt-dlp 等子进程）应对齐的代理地址：显式设置
// 时原样返回；否则经 netproxy 解析系统/环境代理（与共享客户端同源），
// 解析不到返回空串（yt-dlp 自行跟随环境变量）。
func ProxyTarget() string {
	if p := proxyRaw.Load(); p != nil && *p != "" {
		return *p
	}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "www.youtube.com"}}
	if u, err := netproxy.Proxy(req); err == nil && u != nil {
		return u.String()
	}
	return ""
}

// Probe 用共享客户端探测目标（见 probe.go）。
func Probe(ctx context.Context, rawURL string) (*ProbeResult, error) {
	return probe(ctx, Client(), rawURL, nil)
}

// ProbeWithHeaders 带上下文请求头探测目标（浏览器来源任务用）。
func ProbeWithHeaders(ctx context.Context, rawURL string, headers *Headers) (*ProbeResult, error) {
	return probe(ctx, Client(), rawURL, headers)
}
