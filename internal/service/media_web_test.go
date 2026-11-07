package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/besscroft/knot/internal/engine"
)

// setupMediaWeb 准备一个含已完成任务的测试管理器与媒体服务地址，
// 返回 (doneTaskID, 文件内容)，结束时恢复包级状态。
func setupMediaWeb(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	data := []byte("knot-media-web-test-data")
	if err := os.WriteFile(filepath.Join(dir, "sample.mp4"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	completed := time.Now()
	doneTask := engine.Task{
		ID: "webtest-done", URL: "http://x/sample.mp4", Name: "sample.mp4", Dir: dir,
		Status: engine.StatusDone, Total: int64(len(data)), Downloaded: int64(len(data)),
		Duration: 12.5, CreatedAt: completed, CompletedAt: &completed,
	}
	pausedTask := engine.Task{
		ID: "webtest-paused", URL: "http://x/paused.mp4", Name: "paused.mp4", Dir: dir,
		Status: engine.StatusPaused, CreatedAt: completed,
	}
	store := engine.NewStore(filepath.Join(dir, "tasks.json"))
	if err := store.Save([]engine.Task{doneTask, pausedTask}); err != nil {
		t.Fatal(err)
	}
	m := engine.NewManager(store, 1)
	m.Start()
	t.Cleanup(m.Shutdown)

	mediaMu.Lock()
	oldBase, oldPort, oldLn, oldIP, oldMgr := mediaBaseURL, mediaPort, mediaLn, mediaLanIP, mgr
	mediaBaseURL, mediaPort, mediaLn, mediaLanIP, mgr = "http://127.0.0.1:1", "1", nil, "", m
	mediaMu.Unlock()
	t.Cleanup(func() {
		mediaMu.Lock()
		mediaBaseURL, mediaPort, mediaLn, mediaLanIP, mgr = oldBase, oldPort, oldLn, oldIP, oldMgr
		mediaMu.Unlock()
	})
	return doneTask.ID, data
}

// getMedia 以合法的回环 Host 执行一次请求。
func getMedia(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return getMediaHost(t, path, "127.0.0.1:1")
}

// getMediaHost 以指定 Host 头执行一次请求（Host 白名单测试用）。
func getMediaHost(t *testing.T, path, host string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = host
	rec := httptest.NewRecorder()
	mediaHandler(rec, r)
	return rec
}

func TestMediaWebAccess(t *testing.T) {
	doneID, _ := setupMediaWeb(t)

	if rec := getMedia(t, "/"); rec.Code != http.StatusOK {
		t.Fatalf("首页应 200，got %d", rec.Code)
	}
	rec := getMedia(t, "/")
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("首页 Content-Type 应为 text/html，got %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Knot 文件库") {
		t.Errorf("首页应包含标题，got %q", body[:min(120, len(body))])
	}
	if rec := getMedia(t, "/nowhere"); rec.Code != http.StatusNotFound {
		t.Errorf("未知路径应 404，got %d", rec.Code)
	}
	if rec := getMedia(t, "/file/"+doneID+"?dl=1"); rec.Code != http.StatusOK {
		t.Errorf("下载应 200，got %d", rec.Code)
	}

	// Host 白名单：IP 字面量直连放行，域名拒绝（防 DNS rebinding）。
	if rec := getMediaHost(t, "/", "evil.example.com:1"); rec.Code != http.StatusForbidden {
		t.Errorf("域名 Host 应拒绝，got %d", rec.Code)
	}
	if rec := getMediaHost(t, "/", "192.0.2.10:1"); rec.Code != http.StatusOK {
		t.Errorf("IP 直连 Host 应放行，got %d", rec.Code)
	}
	if rec := getMediaHost(t, "/", "localhost:1"); rec.Code != http.StatusOK {
		t.Errorf("localhost Host 应放行，got %d", rec.Code)
	}
}

func TestMediaWebList(t *testing.T) {
	doneID, data := setupMediaWeb(t)

	rec := getMedia(t, "/api/list")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应 200，got %d", rec.Code)
	}
	var items []webItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("列表解析失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("只应包含已完成任务（paused 排除），got %d 项", len(items))
	}
	it := items[0]
	if it.ID != doneID || it.Name != "sample.mp4" {
		t.Errorf("列表项标识不符: %+v", it)
	}
	if it.Kind != "video" || it.Size != int64(len(data)) || it.Missing {
		t.Errorf("列表项字段不符: kind=%q size=%d missing=%v", it.Kind, it.Size, it.Missing)
	}
	if it.CompletedAt == "" {
		t.Error("完成时间不应为空")
	}
}

func TestMediaWebFileDownload(t *testing.T) {
	doneID, data := setupMediaWeb(t)

	// 下载模式：强制 attachment。
	rec := getMedia(t, "/file/"+doneID+"?dl=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("下载应 200，got %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("下载应带 attachment 头，got %q", cd)
	}
	if rec.Body.String() != string(data) {
		t.Errorf("下载内容不符: %q", rec.Body.String())
	}
	// 普通模式：内联，不带 attachment。
	rec = getMedia(t, "/file/"+doneID)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Disposition") != "" {
		t.Errorf("内联访问不应带 attachment 头，got %d %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	// 不存在的任务。
	if rec := getMedia(t, "/file/none"); rec.Code != http.StatusNotFound {
		t.Errorf("未知任务应 404，got %d", rec.Code)
	}
}

func TestMediaServerRealListen(t *testing.T) {
	dir := t.TempDir()
	oldSettings, oldMgr := settings, mgr
	st, err := loadSettings(filepath.Join(dir, "settings.json"), Settings{DownloadDir: dir, SniffTypes: AllSniffTypes})
	if err != nil {
		t.Fatal(err)
	}
	settings = st
	m := engine.NewManager(engine.NewStore(filepath.Join(dir, "tasks.json")), 1)
	m.Start()
	mgr = m
	t.Cleanup(func() {
		StopMediaServer()
		settings, mgr = oldSettings, oldMgr
		m.Shutdown()
	})

	StartMediaServer()
	base := MediaBase()
	if base == "" || !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("base URL 异常: %q", base)
	}
	// 本机模式：Web 地址即 base 加斜杠。
	if u := WebPageURL(); !strings.HasPrefix(u, base) || !strings.HasSuffix(u, "/") {
		t.Errorf("WebPageURL 异常: %q (base %q)", u, base)
	}
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("真实请求页面应 200，got %d", resp.StatusCode)
	}
	resp2, err := http.Get(base + "/api/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var items []webItem
	if err := json.NewDecoder(resp2.Body).Decode(&items); err != nil || len(items) != 0 {
		t.Errorf("空管理器列表应为 []，got %v %v", items, err)
	}
}
