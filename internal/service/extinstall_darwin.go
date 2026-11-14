//go:build darwin

package service

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/besscroft/knot/internal/service/extbundle"
)

// macOS 平台实现：Edge 支持用户级外部扩展 JSON（免管理员，微软官方文档）；
// Chrome 的外部扩展目录在 /Library 下需 root 且校验属主，v1 仅手动指引；
// Firefox 需签名，同样仅手动。

type darwinBrowser struct {
	id, name string
	appPath  string // /Applications/<X>.app
	extDir   string // 用户级外部扩展目录（空 = 仅手动）
	appName  string // open -a 的应用名
	pageURL  string
}

var darwinBrowsers = []darwinBrowser{
	{
		id: "chrome", name: "Google Chrome",
		appPath: "/Applications/Google Chrome.app",
		pageURL: "chrome://extensions",
	},
	{
		id: "edge", name: "Microsoft Edge",
		appPath: "/Applications/Microsoft Edge.app",
		// 微软文档：用户级 External Extensions 目录，无需管理员。
		extDir:  "Library/Application Support/Microsoft Edge/External Extensions",
		appName: "Microsoft Edge",
		pageURL: "edge://extensions",
	},
	{
		id: "brave", name: "Brave",
		appPath: "/Applications/Brave Browser.app",
		pageURL: "brave://extensions",
	},
	{
		id: "firefox", name: "Firefox",
		appPath: "/Applications/Firefox.app",
		pageURL: "about:debugging#/runtime/this-firefox",
	},
}

// extDetectBrowsers 列出各浏览器检测/安装状态。
func extDetectBrowsers(info extbundle.Meta) []extBrowser {
	home, _ := os.UserHomeDir()
	out := make([]extBrowser, 0, len(darwinBrowsers))
	for _, b := range darwinBrowsers {
		_, installed := extInstalledState(b, info, home)
		out = append(out, extBrowser{
			ID:        b.id,
			Name:      b.name,
			Detected:  dirExists(b.appPath),
			Installed: installed,
			UpToDate:  installed && extJSONVersion(b, info, home) == info.Version,
			Trusted:   b.extDir != "" && extPolicyTrusted(b, info.ID),
			Manual:    b.extDir == "",
		})
	}
	return out
}

// ---- 信任策略（ExtensionInstallAllowlist） ------------------------------
// branded Chrome/Edge (Mac) 同样会把非商店外部扩展禁用；macOS 的用户级
// 策略通过 defaults 写入对应域（Chrome: com.google.Chrome，Edge:
// com.microsoft.edge），无需管理员（未实测，TODO 需真机验证）。

// extPolicyDomain 浏览器的策略域。
func extPolicyDomain(b darwinBrowser) string {
	switch b.id {
	case "edge":
		return "com.microsoft.edge"
	default:
		return "com.google.Chrome"
	}
}

// extPolicyTrusted 报告策略数组里是否已放行插件 ID。
func extPolicyTrusted(b darwinBrowser, id string) bool {
	out, err := exec.Command("defaults", "read", extPolicyDomain(b), "ExtensionInstallAllowlist").Output()
	return err == nil && strings.Contains(string(out), id)
}

// extAuthorizeBrowser 写入放行策略（defaults 数组追加）。
func extAuthorizeBrowser(browserID string) error {
	b, ok := darwinFind(browserID)
	if !ok || b.extDir == "" {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	if extPolicyTrusted(b, extbundle.Info().ID) {
		return nil
	}
	return exec.Command("defaults", "write", extPolicyDomain(b),
		"ExtensionInstallAllowlist", "-array-add", extbundle.Info().ID).Run()
}

// extRemovePolicyValue 从策略数组移除插件 ID（defaults 无删项，重写数组）。
func extRemovePolicyValue(b darwinBrowser) error {
	out, err := exec.Command("defaults", "read", extPolicyDomain(b), "ExtensionInstallAllowlist").Output()
	if err != nil {
		return nil
	}
	kept := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != extbundle.Info().ID {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return exec.Command("defaults", "delete", extPolicyDomain(b), "ExtensionInstallAllowlist").Run()
	}
	args := append([]string{"write", extPolicyDomain(b), "ExtensionInstallAllowlist", "-array"}, kept...)
	return exec.Command("defaults", args...).Run()
}

// extJSONPath 读取/写入用 JSON 文件路径（Edge 专属通道）。
func extJSONPath(b darwinBrowser, home string) string {
	return filepath.Join(home, b.extDir, extbundle.Info().ID+".json")
}

// extInstalledState 读 JSON 安装信息。
func extInstalledState(b darwinBrowser, info extbundle.Meta, home string) (string, bool) {
	if b.extDir == "" {
		return "", false
	}
	data, err := os.ReadFile(extJSONPath(b, home))
	if err != nil {
		return "", false
	}
	var payload struct {
		ExternalVersion string `json:"external_version"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return "", true
	}
	return payload.ExternalVersion, true
}

// extJSONVersion 是 extInstalledState 的轻量重复（Status 里 UpToDate 判断用）。
func extJSONVersion(b darwinBrowser, info extbundle.Meta, home string) string {
	ver, _ := extInstalledState(b, info, home)
	return ver
}

// extInstallBrowser 写 Edge 的用户级外部扩展 JSON。
func extInstallBrowser(browserID string) error {
	b, ok := darwinFind(browserID)
	if !ok || b.extDir == "" {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	home, _ := os.UserHomeDir()
	_, crx, _, err := extPaths()
	if err != nil {
		return err
	}
	if !fileExistsDarwin(crx) {
		return fmt.Errorf("插件文件不存在，请先安装一次：%s", crx)
	}
	dir := filepath.Dir(extJSONPath(b, home))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("无法创建配置目录: %w", err)
	}
	info := extbundle.Info()
	payload, _ := json.MarshalIndent(map[string]string{
		"external_crx":     crx,
		"external_version": info.Version,
	}, "", "  ")
	return os.WriteFile(extJSONPath(b, home), payload, 0o644)
}

// extRemoveBrowser 删除外部扩展 JSON。
func extRemoveBrowser(browserID string) error {
	b, ok := darwinFind(browserID)
	if !ok || b.extDir == "" {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	home, _ := os.UserHomeDir()
	if err := os.Remove(extJSONPath(b, home)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return extRemovePolicyValue(b)
}

// extOpenExtensionsPage 用该浏览器打开扩展管理页。
func extOpenExtensionsPage(browserID string) error {
	b, ok := darwinFind(browserID)
	if !ok {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	if !dirExists(b.appPath) {
		return fmt.Errorf("未找到 %s，无法打开扩展管理页", b.name)
	}
	return exec.Command("open", "-a", b.appName, b.pageURL).Start()
}

func darwinFind(id string) (darwinBrowser, bool) {
	for _, b := range darwinBrowsers {
		if b.id == id {
			return b, true
		}
	}
	return darwinBrowser{}, false
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileExistsDarwin(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// extPreInstall macOS 走外部扩展 JSON 通道；「已移除」标记（profile 的
// extensions.external_uninstalls）对 JSON 通道同样生效，暂待需要时再清理。
func extPreInstall(browserID string) error {
	return nil
}
