package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/besscroft/knot/internal/service/extbundle"
)

// 浏览器插件 API：媒体服务器上供 Knot 浏览器插件调用的写接口。
// 访问控制沿用「绑定地址 + Host 白名单」（见 media.go），另要求请求头
// X-Knot-Client：恶意网页跨域 POST 带不了自定义头（预检不会通过，本服务
// 从不放行 CORS），而插件背景页对已授权 host 免 CORS，可正常携带。
// 与 Web 文件库的「无 token」哲学一致：回环监听 + Host 白名单 + 自定义头。

// extClientHeader 是插件请求必须携带的自定义头（值不校验，存在即可）。
const extClientHeader = "X-Knot-Client"

// extCreateRequest 是 POST /api/create 的请求体。通用资源只需 URL；
// B 站资源携带 bili（页面里提取的播放信息，形状与 biliPayload 一致）。
type extCreateRequest struct {
	URL       string       `json:"url,omitempty"`
	Name      string       `json:"name,omitempty"`
	Format    string       `json:"format,omitempty"`
	Referer   string       `json:"referer,omitempty"`
	Cookie    string       `json:"cookie,omitempty"`
	UserAgent string       `json:"userAgent,omitempty"`
	Bili      *biliPayload `json:"bili,omitempty"`
	QualityID int          `json:"qualityId,omitempty"`
}

// handleAPICreate 创建下载任务：通用 URL 走共享的 createTask，B 站播放
// 信息走 createBiliDashTask（与 BilibiliDownload 同一条管线）。
func handleAPICreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get(extClientHeader) == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var req extCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeExtErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	task, err := extCreateTask(&req)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrNotReady) {
			code = http.StatusServiceUnavailable
		}
		writeExtErr(w, code, err.Error())
		return
	}
	writeExtTask(w, task)
}

// extCreateTask 按请求内容分流：B 站播放信息（DASH/旧版 durl）或通用直链。
func extCreateTask(req *extCreateRequest) (engine.Task, error) {
	if req.Bili != nil {
		return createExtBiliTask(req)
	}
	if strings.TrimSpace(req.URL) == "" {
		return engine.Task{}, errors.New("URL 不能为空")
	}
	return createTask(context.Background(), req.URL, CreateOptions{
		Name:      req.Name,
		Format:    req.Format,
		Referer:   req.Referer,
		Cookie:    req.Cookie,
		UserAgent: req.UserAgent,
	})
}

// createExtBiliTask 用插件带来的播放信息创建任务，镜像 BilibiliDownload
// 的分流：DASH → dash 双流任务；旧版 durl → 通用单文件任务。
func createExtBiliTask(req *extCreateRequest) (engine.Task, error) {
	p := req.Bili
	pageURL := strings.TrimSpace(req.Referer)
	if !isBilibiliVideoPage(pageURL) {
		return engine.Task{}, errors.New("来源不是哔哩哔哩视频页")
	}
	if !p.Dash {
		if len(p.Durl) == 0 || p.Durl[0].URL == "" {
			return engine.Task{}, errors.New("未获取到视频地址")
		}
		return createTask(context.Background(), p.Durl[0].URL, CreateOptions{
			Name:      req.Name,
			Format:    req.Format,
			Referer:   pageURL,
			Cookie:    req.Cookie,
			UserAgent: req.UserAgent,
		})
	}
	if !p.hasStreams() {
		return engine.Task{}, errors.New("页面尚未加载播放信息，请刷新后重试")
	}
	// Cookie/UA 由插件从真实浏览器会话取得；防盗链上下文必须来自请求。
	hdrs := &httpdl.Headers{Referer: pageURL, Cookie: req.Cookie, UserAgent: req.UserAgent}
	return createBiliDashTask(p, req.QualityID, req.Format, pageURL, req.Name, hdrs)
}

// serveAPIPing 连通检测：插件设置页「测试连接」用。
func serveAPIPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// serveExtUpdates 输出 gupdate 更新清单（Edge 自托管 update_url 兜底通道）。
// codebase 用实际监听地址动态渲染（绑定失败回退随机端口也保持正确）。
func serveExtUpdates(w http.ResponseWriter, r *http.Request) {
	base := MediaBase()
	info := extbundle.Info()
	if base == "" || info.ID == "" || info.Version == "" {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='%s'>
    <updatecheck codebase='%s/ext/knot-extension.crx' version='%s' />
  </app>
</gupdate>`, info.ID, base, info.Version)
}

// serveExtCrx 直出内嵌的插件 crx3 安装包。
func serveExtCrx(w http.ResponseWriter, r *http.Request) {
	info := extbundle.Info()
	if info.Version == "" {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	// 内容随应用版本变化，禁缓存避免浏览器拿到旧包。
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "knot-extension.crx", time.Now(),
		bytes.NewReader(extbundle.CRX))
}

// writeExtTask 回传创建好的任务（engine.Task 即前端的任务 wire format）。
func writeExtTask(w http.ResponseWriter, task engine.Task) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		OK   bool        `json:"ok"`
		Task engine.Task `json:"task"`
	}{OK: true, Task: task})
}

// writeExtErr 回传错误信息（JSON，供插件展示中文提示）。
func writeExtErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}{OK: false, Error: msg})
}
