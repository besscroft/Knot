package service

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/besscroft/knot/internal/service/extbundle"
)

func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zip 根直出：条目原样落位。
func TestUnzipToFlat(t *testing.T) {
	dst := t.TempDir()
	data := zipBytes(t, map[string]string{"manifest.json": "{}", "bg.js": "1"})
	if err := unzipTo(data, dst); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "bg.js"} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Fatalf("缺少 %s: %v", f, err)
		}
	}
}

// 单一顶层目录：剥掉归一化（wxt zip 若带目录壳也能正确解出）。
func TestUnzipToSingleRoot(t *testing.T) {
	dst := t.TempDir()
	data := zipBytes(t, map[string]string{"chrome-mv3/manifest.json": "{}", "chrome-mv3/a/b.js": "1"})
	if err := unzipTo(data, dst); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "a/b.js"} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Fatalf("缺少 %s: %v", f, err)
		}
	}
}

// 路径穿越条目必须拒绝，不得写出目标目录。
func TestUnzipToRejectsTraversal(t *testing.T) {
	dst := t.TempDir()
	data := zipBytes(t, map[string]string{"a/../../evil.txt": "x"})
	if err := unzipTo(data, dst); err == nil {
		t.Fatal("穿越条目应报错")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "evil.txt")); err == nil {
		t.Fatal("穿越条目不应落盘")
	}
}

// 真实内嵌 zip 应能解出 manifest.json（单根或平铺均可）。
func TestUnzipEmbeddedZip(t *testing.T) {
	dst := t.TempDir()
	if err := unzipTo(extbundle.ZIP, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "manifest.json")); err != nil {
		t.Fatal("内嵌 zip 解压后缺 manifest.json")
	}
}

func setMediaBase(t *testing.T, base string) {
	t.Helper()
	mediaMu.Lock()
	old := mediaBaseURL
	mediaBaseURL = base
	mediaMu.Unlock()
	t.Cleanup(func() {
		mediaMu.Lock()
		mediaBaseURL = old
		mediaMu.Unlock()
	})
}

// /ext/updates.xml：gupdate 清单应包含 appid/version/本机 codebase。
func TestServeExtUpdates(t *testing.T) {
	newTestEnv(t)
	setMediaBase(t, "http://127.0.0.1:22333")
	rec := callMedia(t, http.MethodGet, "/ext/updates.xml", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("updates.xml 状态码 = %d", rec.Code)
	}
	body := rec.Body.String()
	info := extbundle.Info()
	for _, want := range []string{info.ID, info.Version, "http://127.0.0.1:22333/ext/knot-extension.crx", "gupdate"} {
		if !strings.Contains(body, want) {
			t.Fatalf("updates.xml 缺少 %q：%s", want, body)
		}
	}
}

// 媒体服务器未就绪时 updates.xml 应 503；crx 是静态内容，始终 200。
func TestServeExtNotReady(t *testing.T) {
	newTestEnv(t)
	setMediaBase(t, "")
	rec := callMedia(t, http.MethodGet, "/ext/updates.xml", "", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("updates.xml 状态码 = %d，want 503", rec.Code)
	}
	rec = callMedia(t, http.MethodGet, "/ext/knot-extension.crx", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("crx 状态码 = %d，want 200", rec.Code)
	}
}

// /ext/knot-extension.crx 应原样直出内嵌包。
func TestServeExtCrx(t *testing.T) {
	newTestEnv(t)
	setMediaBase(t, "http://127.0.0.1:22333")
	rec := callMedia(t, http.MethodGet, "/ext/knot-extension.crx", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("crx 状态码 = %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), extbundle.CRX) {
		t.Fatalf("crx 内容不符: got %d bytes, want %d", rec.Body.Len(), len(extbundle.CRX))
	}
}
