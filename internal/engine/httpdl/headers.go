package httpdl

import "net/http"

// Headers 是随任务携带的浏览上下文请求头（Referer/Cookie/UA），
// 用于规避站点防盗链；nil 或零值表示用默认。
type Headers struct {
	Referer   string
	Cookie    string
	UserAgent string
}

// Apply 把上下文头写进请求。
func (h *Headers) Apply(req *http.Request) {
	if h == nil {
		return
	}
	if h.UserAgent != "" {
		req.Header.Set("User-Agent", h.UserAgent)
	}
	if h.Referer != "" {
		req.Header.Set("Referer", h.Referer)
	}
	if h.Cookie != "" {
		req.Header.Set("Cookie", h.Cookie)
	}
}
