//go:build windows

package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/besscroft/knot/internal/proc"
	"github.com/besscroft/knot/internal/service/extbundle"
	"golang.org/x/sys/windows/registry"
)

// Windows 平台实现：检测走 App Paths 注册表（HKLM 32 位视图 + HKCU）与
// 常见安装路径兜底；安装信息写 HKCU 下各浏览器的外部扩展 hive
// （external_registry_loader_win.cc：HKCU 免管理员，KEY_NOTIFY 热加载）。
// Chromium 系（Chrome/Brave/Chromium）都读 Google 的 hive；Edge 读自家。

type winBrowser struct {
	id, name      string
	registryKey   string   // HKCU 下的外部扩展 hive（空 = 仅手动）
	appPaths      string   // App Paths 键名（如 chrome.exe）
	userData      string   // User Data 根目录（清「已移除」标记用，空 = 跳过）
	fallbacks     []string // App Paths 找不到时的常见安装路径
	extensionsURL string   // 扩展管理页
}

var winBrowsers = []winBrowser{
	{
		id: "chrome", name: "Google Chrome",
		registryKey:   `Software\Google\Chrome\Extensions`,
		appPaths:      "chrome.exe",
		userData:      `%LocalAppData%\Google\Chrome\User Data`,
		fallbacks:     []string{`%ProgramFiles%\Google\Chrome\Application\chrome.exe`, `%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe`, `%LocalAppData%\Google\Chrome\Application\chrome.exe`},
		extensionsURL: "chrome://extensions",
	},
	{
		id: "edge", name: "Microsoft Edge",
		registryKey:   `Software\Microsoft\Edge\Extensions`,
		appPaths:      "msedge.exe",
		userData:      `%LocalAppData%\Microsoft\Edge\User Data`,
		fallbacks:     []string{`%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe`, `%ProgramFiles%\Microsoft\Edge\Application\msedge.exe`},
		extensionsURL: "edge://extensions",
	},
	{
		id: "brave", name: "Brave",
		// Brave 尚未切到自家 hive，跟随 Chromium 读 Google 的键（brave-browser#37939）。
		registryKey:   `Software\Google\Chrome\Extensions`,
		appPaths:      "brave.exe",
		userData:      `%LocalAppData%\BraveSoftware\Brave-Browser\User Data`,
		fallbacks:     []string{`%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe`, `%LocalAppData%\BraveSoftware\Brave-Browser\Application\brave.exe`},
		extensionsURL: "brave://extensions",
	},
	{
		id: "chromium", name: "Chromium",
		registryKey:   `Software\Google\Chrome\Extensions`,
		appPaths:      "chromium.exe",
		userData:      `%LocalAppData%\Chromium\User Data`,
		fallbacks:     []string{`%LocalAppData%\Chromium\Application\chrome.exe`, `%ProgramFiles%\Chromium\Application\chrome.exe`},
		extensionsURL: "chrome://extensions",
	},
	{
		id: "firefox", name: "Firefox",
		appPaths:      "firefox.exe",
		fallbacks:     []string{`%ProgramFiles%\Mozilla Firefox\firefox.exe`, `%LocalAppData%\Mozilla Firefox\firefox.exe`},
		extensionsURL: "about:debugging#/runtime/this-firefox",
	},
}

// extDetectBrowsers 列出各浏览器检测/安装状态。
func extDetectBrowsers(info extbundle.Meta) []extBrowser {
	out := make([]extBrowser, 0, len(winBrowsers))
	for _, b := range winBrowsers {
		ver, installed := extInstalledState(b, info)
		out = append(out, extBrowser{
			ID:        b.id,
			Name:      b.name,
			Detected:  extExePath(b) != "",
			Installed: installed,
			UpToDate:  installed && ver == info.Version,
			Trusted:   b.registryKey != "" && extPolicyTrusted(b, info.ID),
			Manual:    b.registryKey == "",
		})
	}
	return out
}

// ---- 信任策略（ExtensionInstallAllowlist） ------------------------------
// branded Chrome (Win/Mac) 的信任判定链（Chrome 137+ 实测 157 行为一致）：
//  1. InstallVerifier：非商店外部扩展 → DISABLE_NOT_VERIFIED，即「未在商店
//     列出」红条，且用户点开关会被拒绝（verify 服务器会把不在商店的 ID 判
//     进 invalid_ids，manifest 写 update_url 冒充商店来源也躲不过，反而会
//     触发内容校验 → DISABLE_CORRUPTED，见 wxt.config.ts 注释）。
//  2. 把插件 ID 写入 ExtensionInstallAllowlist 后
//     install_verifier.AllowedByEnterprisePolicy 在一切校验之前短路，扩展
//     可正常启用；服务端缓存过的 invalid_ids 判决也被它豁免。
//  3. 首次注册表安装仍会被浏览器挂 DISABLE_EXTERNAL_EXTENSION（外部来源
//     确认门，Chrome 强制一次用户确认，无法程序化绕过）：用户在扩展页点
//     一次「启用」即确认并持久化，之后的版本更新全自动生效。
// 策略键部分机器 ACL 受限（HKCU\Software\Policies 可能只给 Administrators
// 写权限）→ 非提权失败即 UAC 提权。Allowlist 只按 ID 标记 kAllowed，不会
// 拦截用户安装的其他扩展（默认拦截只由 InstallBlocklist "*" 触发）。

// extPolicyPath 浏览器的信任策略键（Chromium 系按 vendor 分 hive）。
func extPolicyPath(b winBrowser) string {
	vendor := `Google\Chrome`
	switch b.id {
	case "edge":
		vendor = `Microsoft\Edge`
	case "brave":
		vendor = `BraveSoftware\Brave`
	}
	return `Software\Policies\` + vendor + `\ExtensionInstallAllowlist`
}

// extPolicyTrusted 报告策略键中是否已放行插件 ID。
func extPolicyTrusted(b winBrowser, id string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, extPolicyPath(b), registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return false
	}
	for _, name := range names {
		v, _, err := k.GetStringValue(name)
		if err == nil && v == id {
			return true
		}
	}
	return false
}

// extPolicyNextIndex 取下一个空闲序号（值名从 1 递增）。
func extPolicyNextIndex(k registry.Key) (string, error) {
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return "", err
	}
	max := 0
	for _, name := range names {
		if n, err := strconv.Atoi(name); err == nil && n > max {
			max = n
		}
	}
	return strconv.Itoa(max + 1), nil
}

// extWritePolicy 非提权直接写策略；键受限时返回错误（调用方提权重试）。
func extWritePolicy(b winBrowser) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, extPolicyPath(b), registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	idx, err := extPolicyNextIndex(k)
	if err != nil {
		return err
	}
	return k.SetStringValue(idx, extbundle.Info().ID)
}

// extAuthorizeBrowser 放行信任策略：先非提权，受限则 UAC 提权执行。
func extAuthorizeBrowser(browserID string) error {
	b, ok := winFind(browserID)
	if !ok || b.registryKey == "" {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	if extPolicyTrusted(b, extbundle.Info().ID) {
		return nil
	}
	if err := extWritePolicy(b); err == nil {
		return nil
	}
	// 非提权失败：reg.exe 走 UAC（同一用户提权，HKCU 仍指向本用户）。
	// powershell 与提权的 reg.exe 均隐藏控制台（-WindowStyle Hidden），
	// 只让 UAC 确认框出现。
	idxKey, _, err := registry.CreateKey(registry.CURRENT_USER, extPolicyPath(b), registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("无法读取策略键: %w", err)
	}
	idx, err := extPolicyNextIndex(idxKey)
	idxKey.Close()
	if err != nil {
		return err
	}
	ps := fmt.Sprintf(
		`Start-Process reg.exe -ArgumentList 'add','%s','/v','%s','/t','REG_SZ','/d','%s','/f' -Verb RunAs -Wait -WindowStyle Hidden`,
		extPolicyPath(b), idx, extbundle.Info().ID)
	if out, err := proc.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput(); err != nil {
		return fmt.Errorf("管理员授权未完成: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if !extPolicyTrusted(b, extbundle.Info().ID) {
		return fmt.Errorf("授权未生效，请重试")
	}
	return nil
}

// extRemovePolicyValue 从策略键移除插件 ID（值受限时同样走 UAC）。
func extRemovePolicyValue(b winBrowser) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, extPolicyPath(b), registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil // 键不存在视为已清理
	}
	names, _ := k.ReadValueNames(-1)
	target := ""
	for _, name := range names {
		v, _, err := k.GetStringValue(name)
		if err == nil && v == extbundle.Info().ID {
			target = name
			break
		}
	}
	k.Close()
	if target == "" {
		return nil
	}
	// 先尝试非提权删除。
	if wk, err := registry.OpenKey(registry.CURRENT_USER, extPolicyPath(b), registry.SET_VALUE); err == nil {
		err = wk.DeleteValue(target)
		wk.Close()
		if err == nil {
			return nil
		}
	}
	// 值受限：提权删除（同样全程隐藏控制台）。
	ps := fmt.Sprintf(
		`Start-Process reg.exe -ArgumentList 'delete','%s','/v','%s','/f' -Verb RunAs -Wait -WindowStyle Hidden`,
		extPolicyPath(b), target)
	if out, err := proc.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput(); err != nil {
		return fmt.Errorf("移除信任策略失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// extExePath 定位浏览器 exe：App Paths（HKLM 32 位视图 → HKCU）→ 兜底路径。
func extExePath(b winBrowser) string {
	if b.appPaths != "" {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+b.appPaths, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			val, _, err := k.GetStringValue("")
			_ = k.Close()
			if err == nil && val != "" && fileExists(val) {
				return val
			}
		}
	}
	for _, tmpl := range b.fallbacks {
		p := os.ExpandEnv(tmpl)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// extInstalledState 读安装信息：注册表键存在即已安装，version 与内嵌一致即最新。
func extInstalledState(b winBrowser, info extbundle.Meta) (string, bool) {
	if b.registryKey == "" {
		return "", false
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, b.registryKey+`\`+info.ID, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	ver, _, err := k.GetStringValue("version")
	if err != nil {
		return "", true // 有键无版本值：视为已装但未知版本
	}
	return ver, true
}

// extInstallBrowser 写 HKCU 安装信息：path 指向 crx3，version 驱动重装。
func extInstallBrowser(browserID string) error {
	b, ok := winFind(browserID)
	if !ok || b.registryKey == "" {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	_, crx, _, err := extPaths()
	if err != nil {
		return err
	}
	if !fileExists(crx) {
		return fmt.Errorf("插件文件不存在，请先安装一次：%s", crx)
	}
	info := extbundle.Info()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, b.registryKey+`\`+info.ID, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("写入注册表失败: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue("path", crx); err != nil {
		return err
	}
	return k.SetStringValue("version", info.Version)
}

// extPreInstall 安装前置：清理「用户已移除」标记。用户在扩展页点过「移除」
// 后，浏览器会把插件 ID 记进各 profile Preferences 的
// extensions.external_uninstalls（带完整性哈希），此后注册表重装会被
// external_provider_manager 静默跳过——无日志无提示，表现为「安装了但浏览器
// 找不到」。标记只读得到写不得（浏览器运行中的改动会在退出时被覆写），
// 所以标记存在且浏览器在跑时要求先退出。返回 nil 表示无需处理或已清理。
func extPreInstall(browserID string) error {
	b, ok := winFind(browserID)
	if !ok || b.registryKey == "" || b.userData == "" {
		return nil
	}
	if !extHasRemovedFlag(b) {
		return nil
	}
	if extBrowserRunning(b) {
		return fmt.Errorf("%s 正在运行，且配置里残留「已移除本插件」标记（会让浏览器静默拒绝重装）。请完全退出 %s（含托盘/后台进程）后重新点「安装」", b.name, b.name)
	}
	if _, err := extCleanRemovedFlag(b); err != nil {
		return fmt.Errorf("清理浏览器移除标记失败: %w", err)
	}
	return nil
}

// extBrowserRunning 用镜像名探测浏览器进程（tasklist 对无匹配会输出本地化
// 的「没有运行的任务」，所以按进程名出现与否判断，不解析行数）。
func extBrowserRunning(b winBrowser) bool {
	if b.appPaths == "" {
		return false
	}
	out, err := proc.Command("tasklist", "/FI", "IMAGENAME eq "+b.appPaths, "/NH").CombinedOutput()
	return err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(b.appPaths))
}

// extHasRemovedFlag 报告任一 profile 是否残留「已移除」标记。
func extHasRemovedFlag(b winBrowser) bool {
	hit, err := extCleanRemovedFlag(b)
	return hit && err == nil
}

// extCleanRemovedFlag 遍历浏览器 User Data 下所有 profile 的 Preferences，
// 从 extensions.external_uninstalls 里摘除插件 ID，返回是否实际清理过。
// 只在浏览器退出时调用。JSON 往返必须 UseNumber：Preferences 里有超过
// float64 精度安全区的整数字段，map[string]any 直解会把它们写成浮点近似值。
func extCleanRemovedFlag(b winBrowser) (bool, error) {
	id := extbundle.Info().ID
	root := os.ExpandEnv(b.userData)
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, nil // 根目录不存在 = 浏览器没有本地数据
	}
	cleaned := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		prefsPath := filepath.Join(root, e.Name(), "Preferences")
		data, err := os.ReadFile(prefsPath)
		if err != nil {
			continue
		}
		var prefs map[string]any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if dec.Decode(&prefs) != nil {
			continue
		}
		ext, ok := prefs["extensions"].(map[string]any)
		if !ok {
			continue
		}
		list, ok := ext["external_uninstalls"].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(list))
		hit := false
		for _, v := range list {
			if s, _ := v.(string); s == id {
				hit = true
				continue
			}
			kept = append(kept, v)
		}
		if !hit {
			continue
		}
		if len(kept) == 0 {
			delete(ext, "external_uninstalls")
		} else {
			ext["external_uninstalls"] = kept
		}
		out, err := json.Marshal(prefs)
		if err != nil {
			return cleaned, err
		}
		if err := os.WriteFile(prefsPath, out, 0o644); err != nil {
			return cleaned, err
		}
		cleaned = true
	}
	return cleaned, nil
}

// extRemoveBrowser 删除 HKCU 安装信息（浏览器重启后扩展消失）。
func extRemoveBrowser(browserID string) error {
	b, ok := winFind(browserID)
	if !ok || b.registryKey == "" {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	key := b.registryKey + `\` + extbundle.Info().ID
	if err := registry.DeleteKey(registry.CURRENT_USER, key); err != nil {
		if err == registry.ErrNotExist {
			return extRemovePolicyValue(b)
		}
		return fmt.Errorf("删除注册表失败: %w", err)
	}
	return extRemovePolicyValue(b)
}

// extOpenExtensionsPage 用该浏览器打开扩展管理页（已运行则开新标签）。
func extOpenExtensionsPage(browserID string) error {
	b, ok := winFind(browserID)
	if !ok {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	exe := extExePath(b)
	if exe == "" {
		return fmt.Errorf("未找到 %s，无法打开扩展管理页", b.name)
	}
	return proc.Command(exe, b.extensionsURL).Start()
}

func winFind(id string) (winBrowser, bool) {
	for _, b := range winBrowsers {
		if b.id == id {
			return b, true
		}
	}
	return winBrowser{}, false
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	// App Paths 的默认值可能带引号或参数，直接展开失败时按可执行名在 PATH 里找。
	if filepath.IsAbs(path) {
		return false
	}
	_, err := exec.LookPath(path)
	return err == nil
}
