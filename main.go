// Knot 是一个基于 MyGo 的资源下载器。
package main

import (
	"log"
	"runtime"

	"github.com/besscroft/knot/internal/service"
	"github.com/egoist/mygo"
)

func main() {
	// 第二个实例启动时直接退出，由已运行的实例接管。
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	mygo.Bind(&service.Tasks{})
	mygo.BindAs("App", &service.AppService{})
	mygo.BindAs("Browser", &service.Browser{})

	mygo.App.WhenReady(func() {
		// 存储与队列的初始化必须在 WhenReady 里：`mygo generate` 也会运行
		// 到这里之前，不能在包初始化时触碰用户数据目录。
		if err := service.Init(); err != nil {
			service.Fatal("初始化失败", err)
			mygo.App.Quit()
			return
		}
		service.SetMainWindow(createMainWindow())
	})

	// Windows/Linux：最后一个窗口关闭即退出；macOS：保持运行，激活时重建窗口。
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			service.SetMainWindow(createMainWindow())
		}
	})
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		if w := service.MainWindow(); w != nil {
			w.Restore()
			w.Focus()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		service.Shutdown() // 停任务、落盘断点状态
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// createMainWindow 创建主窗口（首次启动与 macOS 激活时共用）。
func createMainWindow() *mygo.Window {
	return mygo.NewWindow(mygo.WindowOptions{
		Title:           "Knot",
		Width:           1200,
		Height:          800,
		MinWidth:        960,
		MinHeight:       640,
		BackgroundColor: "light-dark(#f7f8fa, #141414)", // 与前端 --bg 一致
		// 记住上次的位置与大小。
		StateKey: "main",
		// dev 时加载 devUrl（Vite+ 开发服务器），打包后加载内嵌的 frontendDist。
		URL: "/",
	})
}
