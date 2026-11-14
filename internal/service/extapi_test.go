package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/besscroft/knot/internal/engine"
)

// newTestEnv 搭最小可用环境：临时目录里的设置与任务管理器。
// 测试结束按注册顺序倒序清理：先关服务器、再停任务、最后删目录。
func newTestEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	def := Settings{
		DownloadDir:   dir,
		Threads:       4,
		MaxConcurrent: 2,
		WebPort:       DefaultWebPort,
		SniffTypes:    AllSniffTypes,
	}
	store, err := loadSettings(filepath.Join(dir, "settings.json"), def)
	if err != nil {
		t.Fatalf("加载测试设置失败: %v", err)
	}
	settings = store
	mgr = engine.NewManager(engine.NewStore(filepath.Join(dir, "tasks.json")), 2)
	t.Cleanup(func() {
		mgr.Shutdown()
		mgr = nil
		settings = nil
	})
}

// mediaServer 起一个媒体处理器实例（手动指定本机 Host 以过白名单），
// 并附带一个提供任意路径同一文件内容的下载源。
func mediaServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x.mp4", time.Unix(0, 0), bytes.NewReader(bytes.Repeat([]byte("k"), 4096)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callMedia(t *testing.T, method, path, body string, clientHeader bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "127.0.0.1:22333" // 过 Host 白名单
	if clientHeader {
		req.Header.Set(extClientHeader, "test")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mediaHandler(rec, req)
	return rec
}

func TestAPIPing(t *testing.T) {
	rec := callMedia(t, http.MethodGet, "/api/ping", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("ping 状态码 = %d， want 200", rec.Code)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.OK {
		t.Fatalf("ping 响应异常: %s", rec.Body.String())
	}
}

// 恶意域名经 DNS rebinding 指过来的请求应被 Host 白名单拦下。
func TestAPIPingRejectsForeignHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Host = "evil.example.com:22333"
	rec := httptest.NewRecorder()
	mediaHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("外部 Host 状态码 = %d，want 403", rec.Code)
	}
}

// 缺自定义头的跨站 POST 必须拒绝（网页无法携带自定义头，防 drive-by）。
func TestAPICreateRequiresClientHeader(t *testing.T) {
	newTestEnv(t)
	rec := callMedia(t, http.MethodPost, "/api/create", `{"url":"https://example.com/a.mp4"}`, false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无自定义头状态码 = %d，want 403", rec.Code)
	}
}

func TestAPICreateGeneric(t *testing.T) {
	newTestEnv(t)
	srv := mediaServer(t)
	body, _ := json.Marshal(map[string]any{"url": srv.URL + "/movie.mp4"})
	rec := callMedia(t, http.MethodPost, "/api/create", string(body), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create 状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK   bool        `json:"ok"`
		Task engine.Task `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if !out.OK || out.Task.ID == "" {
		t.Fatalf("任务创建失败: %s", rec.Body.String())
	}
	if out.Task.Name != "movie.mp4" {
		t.Fatalf("文件名 = %q，want movie.mp4", out.Task.Name)
	}
}

// B 站播放信息走 DASH 分支：挑流、探活、建 dash 双流任务。
func TestAPICreateBiliDash(t *testing.T) {
	newTestEnv(t)
	srv := mediaServer(t)
	payload := map[string]any{
		"referer":   "https://www.bilibili.com/video/BV1test",
		"qualityId": 80,
		"bili": map[string]any{
			"ok":         true,
			"dash":       true,
			"timelength": 60000,
			"acceptIds":  []any{80, 64},
			"acceptDesc": []any{"1080P 高清", "720P 高清"},
			"videos": []any{map[string]any{
				"id": 80, "codecs": "avc1.640032", "bandwidth": 2000000,
				"base": srv.URL + "/video.m4s",
			}},
			"audios": []any{map[string]any{
				"id": 30280, "codecs": "mp4a.40.2", "bandwidth": 320000,
				"base": srv.URL + "/audio.m4s",
			}},
		},
	}
	body, _ := json.Marshal(payload)
	rec := callMedia(t, http.MethodPost, "/api/create", string(body), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create(dash) 状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK   bool        `json:"ok"`
		Task engine.Task `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if !out.OK {
		t.Fatalf("任务创建失败: %s", rec.Body.String())
	}
	if out.Task.Kind != "dash" {
		t.Fatalf("任务类型 = %q，want dash", out.Task.Kind)
	}
	if out.Task.AudioURL == "" {
		t.Fatal("dash 任务缺少音频流地址")
	}
	if out.Task.Referer != "https://www.bilibili.com/video/BV1test" {
		t.Fatalf("Referer = %q", out.Task.Referer)
	}
	if out.Task.Total <= 0 {
		t.Fatalf("探测后的总大小应 > 0，got %d", out.Task.Total)
	}
}

// B 站旧版 durl（无 dash）回落到通用创建流程。
func TestAPICreateBiliDurl(t *testing.T) {
	newTestEnv(t)
	srv := mediaServer(t)
	body, _ := json.Marshal(map[string]any{
		"referer": "https://www.bilibili.com/video/BV1test",
		"bili": map[string]any{
			"ok":   true,
			"dash": false,
			"durl": []any{map[string]any{"url": srv.URL + "/legacy.flv", "size": 4096}},
		},
	})
	rec := callMedia(t, http.MethodPost, "/api/create", string(body), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create(durl) 状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK   bool        `json:"ok"`
		Task engine.Task `json:"task"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.OK || out.Task.Kind != "" {
		t.Fatalf("durl 应建普通直链任务: %s", rec.Body.String())
	}
}

// 来源不是 B 站视频页时拒绝，防任意 URL 借道 dash 分支绕过探测。
func TestAPICreateBiliRejectsForeignReferer(t *testing.T) {
	newTestEnv(t)
	body, _ := json.Marshal(map[string]any{
		"referer": "https://evil.example.com/video/x",
		"bili":    map[string]any{"ok": true, "dash": true},
	})
	rec := callMedia(t, http.MethodPost, "/api/create", string(body), true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非 B 站来源状态码 = %d，want 400", rec.Code)
	}
}
