// 托盘常驻：系统托盘图标 + 菜单（打开主窗口 / 暂停·继续全部任务 / 退出），
// 以及「关闭窗口 = 隐藏到托盘」的首次隐藏提示。托盘由 main.go 在 WhenReady
// 里创建；创建失败只记录，不阻断应用启动。
package service

import (
	_ "embed"
	"log"
	"sync"

	"github.com/besscroft/knot/internal/engine"
	"github.com/egoist/mygo"
)

//go:embed assets/tray.png
var trayIcon []byte

// hideHintOnce 保证「已最小化到托盘」的提示每次运行只弹一次。
var hideHintOnce sync.Once

// InitTray 创建系统托盘。须在 App 就绪后调用（main.go 的 WhenReady 内），
// onShow 在「打开」菜单项里唤起主窗口。
func InitTray(onShow func()) error {
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 Knot", Click: func(*mygo.MenuItem, *mygo.Window) {
			if onShow != nil {
				onShow()
			}
		}},
		mygo.Separator(),
		// Pause 对 running 任务会等 goroutine 退出，放 goroutine 里跑，
		// 任务多时不卡菜单线程。
		{Label: "暂停全部任务", Click: func(*mygo.MenuItem, *mygo.Window) { go pauseAll() }},
		{Label: "继续全部任务", Click: func(*mygo.MenuItem, *mygo.Window) { go resumeAll() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit}, // 标准退出：走 Quit → OnBeforeQuit → service.Shutdown
	})
	if _, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "knot-tray",
		Icon:    trayIcon,
		ToolTip: "Knot",
		Menu:    menu,
	}); err != nil {
		return err
	}
	return nil
}

// pauseAll 暂停所有进行中（running/pending）的任务。
func pauseAll() {
	if mgr == nil {
		return
	}
	for _, t := range mgr.List() {
		if t.Status == engine.StatusRunning || t.Status == engine.StatusPending {
			if err := mgr.Pause(t.ID); err != nil {
				log.Println("托盘暂停任务失败:", err)
			}
		}
	}
}

// resumeAll 恢复所有暂停中的任务。
func resumeAll() {
	if mgr == nil {
		return
	}
	for _, t := range mgr.List() {
		if t.Status == engine.StatusPaused {
			if err := mgr.Resume(t.ID); err != nil {
				log.Println("托盘恢复任务失败:", err)
			}
		}
	}
}

// notifyHideHint 在窗口首次被关闭隐藏到托盘时弹一次系统通知，告诉用户
// 应用还在运行、如何退出。通知失败静默忽略（如系统未授权）。
func notifyHideHint() {
	hideHintOnce.Do(func() {
		if !mygo.NotificationsSupported() {
			return
		}
		mygo.NewNotification(mygo.NotificationOptions{
			Title:  "Knot 已最小化到托盘",
			Body:   "下载将在后台继续；点击托盘图标可打开主窗口，菜单里可退出应用。",
			Silent: true,
		}).Show()
	})
}
