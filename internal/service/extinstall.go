package service

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/besscroft/knot/internal/service/extbundle"
	"github.com/egoist/mygo"
)

// 浏览器插件安装：把内嵌的 crx3 分发给本机已装的 Chromium 系浏览器。
// 机制（external_registry_loader_win.cc / Edge 官方文档）：注册表写入
// path(.crx)+version（Chromium 系含 Brave/Chromium 读 Google 的 hive，
// Edge 读自家 hive），浏览器通过 KEY_NOTIFY 监听热加载或下次启动装载；
// version 变化即重装，天然承担升级。Firefox 需 Mozilla 签名，仅检测 +
// 手动指引；macOS 走 Edge 的用户级 External Extensions JSON（见
// extinstall_darwin.go）。无需管理员：Windows 写 HKCU、macOS 写用户目录。
//
// 信任放行（InstallVerifier，branded Chrome/Edge Win/Mac 强制）：非商店
// 外部扩展会被以「可能已损坏」（DISABLE_NOT_VERIFIED/suspiciousInstall）
// 禁用，用户无法手动启用；唯一自动通道是把插件 ID 写入企业策略
// ExtensionInstallAllowlist（install_verifier.AllowedByEnterprisePolicy
// 先于商店校验）。策略键在部分机器 ACL 受限，写不进去时走 UAC 提权
// （Ext.Authorize），授权后浏览器可能显示「由您的组织管理」标记。
//
// 平台差异集中在 extinstall_windows.go / extinstall_darwin.go /
// extinstall_other.go，本文件只承载共享逻辑与对外服务。

// extBrowser 是单个浏览器的安装状态。
type extBrowser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Detected  bool   `json:"detected"`  // 本机装了这个浏览器
	Installed bool   `json:"installed"` // 插件已注册到该浏览器
	UpToDate  bool   `json:"upToDate"`  // 已注册且版本与内嵌一致
	Trusted   bool   `json:"trusted"`   // 信任策略已放行（否则会被浏览器禁用）
	Manual    bool   `json:"manual"`    // 无自动通道，仅提示手动加载
}

// ExtStatus 是插件安装状态总览（Ext.Status 的返回值）。
type ExtStatus struct {
	Browsers    []extBrowser `json:"browsers"`
	ID          string       `json:"id"`
	Version     string       `json:"version"`
	Dir         string       `json:"dir"`         // 插件落盘目录
	CrxPath     string       `json:"crxPath"`     // crx3 文件（安装信息指向它）
	UnpackedDir string       `json:"unpackedDir"` // 解压目录（手动 Load unpacked 兜底）
}

// ExtInstaller 是暴露给前端的插件安装服务（frontend/src/mygo.ts 中的 Ext.*）。
type ExtInstaller struct{}

// Status 检测本机浏览器与插件安装状态。文件随 Install 落盘，此方法只读。
func (e *ExtInstaller) Status() (ExtStatus, error) {
	if settings == nil {
		return ExtStatus{}, ErrNotReady
	}
	dir, crx, unpacked, err := extPaths()
	if err != nil {
		return ExtStatus{}, err
	}
	info := extbundle.Info()
	return ExtStatus{
		Browsers:    extDetectBrowsers(info),
		ID:          info.ID,
		Version:     info.Version,
		Dir:         dir,
		CrxPath:     crx,
		UnpackedDir: unpacked,
	}, nil
}

// Install 把插件安装到指定浏览器：落盘 crx/解压目录并写入安装信息。
// 浏览器运行中通常几秒内热加载，否则下次启动生效。
func (e *ExtInstaller) Install(browserID string) error {
	if settings == nil {
		return ErrNotReady
	}
	b := extFindBrowser(browserID)
	if b == nil {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	if b.Manual {
		return fmt.Errorf("%s 不支持自动安装，请手动加载解压目录", b.Name)
	}
	if _, _, _, err := extWriteFiles(); err != nil {
		return err
	}
	return extInstallBrowser(browserID)
}

// Uninstall 移除指定浏览器的插件注册（浏览器重启后扩展消失）。
func (e *ExtInstaller) Uninstall(browserID string) error {
	if settings == nil {
		return ErrNotReady
	}
	b := extFindBrowser(browserID)
	if b == nil || b.Manual {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	return extRemoveBrowser(browserID)
}

// Authorize 写入浏览器的信任放行策略（ExtensionInstallAllowlist）。策略键
// 受限时自动弹 UAC 提权执行；成功后浏览器重载扩展页/下次启动即启用插件。
func (e *ExtInstaller) Authorize(browserID string) error {
	if settings == nil {
		return ErrNotReady
	}
	b := extFindBrowser(browserID)
	if b == nil || b.Manual {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	return extAuthorizeBrowser(browserID)
}

// OpenDir 打开插件落盘目录（手动 Load unpacked 时选 unpacked 子目录）。
func (e *ExtInstaller) OpenDir() error {
	_, _, unpacked, err := extPaths()
	if err != nil {
		return err
	}
	return mygo.Shell.OpenPath(unpacked)
}

// OpenExtensionsPage 打开指定浏览器的扩展管理页（浏览器未运行时会启动它）。
func (e *ExtInstaller) OpenExtensionsPage(browserID string) error {
	b := extFindBrowser(browserID)
	if b == nil || b.Manual {
		return fmt.Errorf("不支持的浏览器: %s", browserID)
	}
	return extOpenExtensionsPage(browserID)
}

// extFindBrowser 按 ID 查浏览器（含实时检测）。
func extFindBrowser(id string) *extBrowser {
	for _, b := range extDetectBrowsers(extbundle.Info()) {
		if b.ID == id {
			return &b
		}
	}
	return nil
}

// extPaths 计算插件落盘路径（不创建任何东西）。
func extPaths() (dir, crx, unpacked string, err error) {
	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", "", "", err
	}
	dir = filepath.Join(userData, "extension")
	return dir,
		filepath.Join(dir, "knot-extension.crx"),
		filepath.Join(dir, "unpacked"),
		nil
}

// extWriteFiles 把内嵌 crx 与解压目录写到 userData（Install 时调用，
// 内容以内存为准直接覆盖，二百多 KB 代价可忽略）。
func extWriteFiles() (dir, crx, unpacked string, err error) {
	dir, crx, unpacked, err = extPaths()
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("无法创建插件目录: %w", err)
	}
	if err := os.WriteFile(crx, extbundle.CRX, 0o644); err != nil {
		return "", "", "", fmt.Errorf("无法写入 crx: %w", err)
	}
	if err := unzipTo(extbundle.ZIP, unpacked); err != nil {
		return "", "", "", fmt.Errorf("无法解压插件目录: %w", err)
	}
	return dir, crx, unpacked, nil
}

// unzipTo 解压 zip 到 dst，内容在 zip 根或单一顶层目录下均可（归一化）。
func unzipTo(data []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	// 归一化：所有条目都挂在同一个顶层目录下时剥掉它。
	root := ""
	allShareRoot := len(zr.File) > 0
	for _, f := range zr.File {
		if f.Mode().IsDir() || !bytes.Contains([]byte(f.Name), []byte("/")) {
			allShareRoot = false // 根下有散文件，不剥
			break
		}
		top := f.Name[:indexByte(f.Name, '/')]
		if root == "" {
			root = top
		} else if root != top {
			allShareRoot = false
			break
		}
	}
	prefix := ""
	if allShareRoot {
		prefix = root + "/"
	}
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		name := f.Name
		if prefix != "" {
			if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
				continue
			}
			name = name[len(prefix):]
		}
		dstPath := filepath.Join(dst, filepath.FromSlash(name))
		// 防路径穿越：条目名本身必须是本目录内的相对路径。
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return errors.New("zip 条目路径异常: " + f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
