package httpdl

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSetProxyValidation(t *testing.T) {
	cases := []struct {
		raw    string
		wantOK bool
	}{
		{"", true},
		{"http://127.0.0.1:7890", true},
		{"https://user:pass@proxy.example.com:8443", true},
		{"socks5://127.0.0.1:1080", true},
		{"127.0.0.1:7890", false}, // 缺协议
		{"ftp://x", false},        // 不支持的协议
		{"http://[bad", false},    // 解析失败
	}
	for _, c := range cases {
		err := SetProxy(c.raw)
		if c.wantOK != (err == nil) {
			t.Errorf("SetProxy(%q) err = %v, wantOK = %v", c.raw, err, c.wantOK)
		}
	}
}

// forwardingProxy 是把绝对式请求原样转发到目标的上游 HTTP 代理。
func forwardingProxy(t *testing.T, hit *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hit++
		if r.URL.Host == "" {
			http.Error(w, "not a proxy request", http.StatusBadRequest)
			return
		}
		out, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		out.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSetProxyRoutesRequests(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello via proxy"))
	}))
	defer target.Close()

	hits := 0
	proxy := forwardingProxy(t, &hits)
	defer proxy.Close()

	if err := SetProxy(proxy.URL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetProxy("") })

	res, err := probe(context.Background(), Client(), target.URL, nil)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.Size != int64(len("hello via proxy")) {
		t.Fatalf("Size = %d", res.Size)
	}
	if hits == 0 {
		t.Fatal("请求没有经过代理")
	}
}

func TestSetProxyInvalidNotApplied(t *testing.T) {
	before := Client()
	if err := SetProxy("ftp://nope"); err == nil {
		t.Fatal("非法代理应报错")
	}
	if Client() != before {
		t.Fatal("校验失败时不应替换客户端")
	}
}

func TestProxyURLWithAuth(t *testing.T) {
	u, err := url.Parse("http://user:pass@127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if u.User == nil || u.User.String() != "user:pass" {
		t.Fatalf("带认证的代理解析异常: %v", u.User)
	}
}
