package service

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/egoist/mygo"
)

// TaskUpdated 把单个任务的最新快照广播给所有窗口。
// 包级声明以便 `mygo generate` 收录进 TypeScript 客户端（events.taskUpdated）。
var TaskUpdated = mygo.NewEvent[engine.Task]("task-updated")

var (
	mgr      *engine.Manager
	settings *settingsStore

	mainMu  sync.Mutex
	mainWin *mygo.Window

	// taskbar 进度聚合所需的运行中任务集合。
	taskbarMu sync.Mutex
	taskbar   = map[string]taskProgress{}
)

type taskProgress struct {
	downloaded int64
	total      int64
}

// ErrNotReady 表示服务尚未初始化（只在 WhenReady 之前才会遇到）。
var ErrNotReady = errors.New("服务尚未就绪")

// Init 在 App.WhenReady 中调用：准备数据目录、加载设置、恢复任务队列。
func Init() error {
	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(userData, 0o755); err != nil {
		return err
	}

	def := Settings{DownloadDir: defaultDownloadDir(), Threads: 8, MaxConcurrent: 3}
	settings, err = loadSettings(filepath.Join(userData, "settings.json"), def)
	if err != nil {
		return err
	}

	store := engine.NewStore(filepath.Join(userData, "tasks.json"))
	mgr = engine.NewManager(store, settings.Get().MaxConcurrent)
	mgr.SetNotifier(onTaskUpdate)
	mgr.Start() // 恢复上次会话的任务并启动进度循环

	// 应用已保存的下载代理；失败仅记录，保持默认直连。
	if err := httpdl.SetProxy(settings.Get().Proxy); err != nil {
		log.Println("应用代理设置失败:", err)
	}

	go sniffLoop() // 浏览中周期性重扫页面资源
	return nil
}

// Shutdown 在退出前停掉所有任务，保证断点状态落盘。
func Shutdown() {
	if mgr != nil {
		mgr.Shutdown()
	}
}

// SetMainWindow 记录主窗口引用：注册窗口钩子（移动/缩放时同步站点与浮窗位置、
// 下载拦截兜底），并在窗口被重建（macOS 激活）时复位浏览状态。
func SetMainWindow(w *mygo.Window) {
	mainMu.Lock()
	mainWin = w
	mainMu.Unlock()

	resetBrowse()

	// 主窗口移动/缩放：站点子窗口与嗅探浮窗跟随内容区。
	w.OnMove(func() {
		browser.mu.Lock()
		browsing := browser.mode == ModeBrowsing
		browser.mu.Unlock()
		if browsing {
			positionSite()
			positionPanel()
		}
	})
	w.OnResize(func() {
		browser.mu.Lock()
		browsing := browser.mode == ModeBrowsing
		browser.mu.Unlock()
		if browsing {
			positionSite()
			positionPanel()
		}
	})
	// 应用自身页面不允许触发下载（站点窗口有自己的拦截逻辑）。
	w.Page().OnWillDownload(func(e *mygo.DownloadEvent) {
		e.PreventDefault()
	})
}

// MainWindow 返回主窗口引用（可能为 nil）。
func MainWindow() *mygo.Window {
	mainMu.Lock()
	defer mainMu.Unlock()
	return mainWin
}

// Fatal 记录致命错误并弹出系统错误对话框。
func Fatal(msg string, err error) {
	log.Println(msg, err)
	mygo.Dialog.Error("Knot", msg+": "+err.Error())
}

// defaultDownloadDir 返回系统下载目录；失败时退回用户目录。
func defaultDownloadDir() string {
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil && dir != "" {
		return dir
	}
	if home, err := mygo.App.Path(mygo.PathHome); err == nil && home != "" {
		return home
	}
	dir, _ := os.UserHomeDir()
	return dir
}

// onTaskUpdate 是 engine → 前端 的通知出口：广播事件 + 聚合任务栏进度。
// 它由 engine 在锁外调用，不得再回调 engine。
func onTaskUpdate(t engine.Task) {
	_ = TaskUpdated.Broadcast(t)

	taskbarMu.Lock()
	switch t.Status {
	case engine.StatusRunning:
		taskbar[t.ID] = taskProgress{downloaded: t.Downloaded, total: t.Total}
	case engine.StatusPending:
		taskbar[t.ID] = taskProgress{downloaded: 0, total: t.Total}
	default:
		delete(taskbar, t.ID)
	}
	var sumDown, sumTotal int64
	for _, p := range taskbar {
		sumDown += p.downloaded
		if p.total > 0 {
			sumTotal += p.total
		}
	}
	taskbarMu.Unlock()

	if mainWin == nil {
		return
	}
	switch {
	case sumTotal <= 0:
		mainWin.SetProgressBar(mygo.ProgressBar{})
	default:
		f := float64(sumDown) / float64(sumTotal)
		if f < 0 {
			f = 0
		} else if f > 1 {
			f = 1
		}
		mainWin.SetProgressBar(mygo.ProgressBar{Value: f})
	}
}
