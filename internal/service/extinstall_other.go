//go:build !windows && !darwin

package service

import (
	"fmt"
	"os/exec"

	"github.com/besscroft/knot/internal/service/extbundle"
)

// 其他平台（Linux 等）的外部扩展目录都需要 root 且位置分散，v1 仅检测
// + 手动指引；检测用 PATH 里的可执行名。

type otherBrowser struct {
	id, name string
	exe      string
	pageURL  string
}

var otherBrowsers = []otherBrowser{
	{id: "chrome", name: "Google Chrome", exe: "google-chrome", pageURL: "chrome://extensions"},
	{id: "edge", name: "Microsoft Edge", exe: "microsoft-edge", pageURL: "edge://extensions"},
	{id: "brave", name: "Brave", exe: "brave-browser", pageURL: "brave://extensions"},
	{id: "chromium", name: "Chromium", exe: "chromium", pageURL: "chrome://extensions"},
	{id: "firefox", name: "Firefox", exe: "firefox", pageURL: "about:debugging#/runtime/this-firefox"},
}

// extDetectBrowsers 列出各浏览器检测/安装状态（全部仅手动）。
func extDetectBrowsers(info extbundle.Meta) []extBrowser {
	out := make([]extBrowser, 0, len(otherBrowsers))
	for _, b := range otherBrowsers {
		_, err := exec.LookPath(b.exe)
		out = append(out, extBrowser{
			ID:       b.id,
			Name:     b.name,
			Detected: err == nil,
			Manual:   true,
		})
	}
	return out
}

func extInstallBrowser(browserID string) error {
	return fmt.Errorf("该平台暂不支持自动安装，请手动加载解压目录")
}

func extRemoveBrowser(browserID string) error {
	return fmt.Errorf("该平台暂不支持自动安装，请手动加载解压目录")
}

func extAuthorizeBrowser(browserID string) error {
	return fmt.Errorf("该平台暂不支持自动安装，请手动加载解压目录")
}

func extOpenExtensionsPage(browserID string) error {
	return fmt.Errorf("该平台暂不支持自动安装，请手动加载解压目录")
}

// extPreInstall 该平台无注册表安装通道，无「已移除」标记可清。
func extPreInstall(browserID string) error {
	return nil
}
