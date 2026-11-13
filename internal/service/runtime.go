package service

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/besscroft/knot/internal/engine/ytdlp"
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
	cacheDir, _ := mygo.App.Path(mygo.PathCache)
	migrateLegacyData(userData, cacheDir) // 旧开发目录的数据一次性并入（不覆盖已有）

	def := Settings{DownloadDir: defaultDownloadDir(), Threads: 8, MaxConcurrent: 3, SniffTypes: AllSniffTypes}
	settings, err = loadSettings(filepath.Join(userData, "settings.json"), def)
	if err != nil {
		return err
	}
	// 把 sanitize 补齐的默认值（如 webPort）立即落盘，早于任何前端保存，
	// 设置文件与实际生效值保持一致（顺带清掉历史遗留的废弃字段）。
	if err := settings.Set(settings.Get()); err != nil {
		log.Println("落盘初始化设置失败:", err)
	}

	store := engine.NewStore(filepath.Join(userData, "tasks.json"))
	mgr = engine.NewManager(store, settings.Get().MaxConcurrent)
	mgr.SetNotifier(onTaskUpdate)
	mgr.Start() // 恢复上次会话的任务并启动进度循环

	// 应用已保存的下载代理；失败仅记录，保持默认直连。
	if err := httpdl.SetProxy(settings.Get().Proxy); err != nil {
		log.Println("应用代理设置失败:", err)
	}

	// FFmpeg：设置指定路径 + 自动下载缓存目录。
	engine.SetFfmpegOptions(settings.Get().FfmpegPath, filepath.Join(cacheDir, "ffmpeg"))
	// yt-dlp（YouTube 下载）：设置指定路径 + 自动下载缓存目录。
	ytdlp.SetOptions(settings.Get().YtDlpPath, filepath.Join(cacheDir, "ytdlp"))

	StartMediaServer() // loopback 媒体服务：文件与封面（大文件必须真流式，见 media.go）

	// 旧版本完成的任务可能没有时长，或流式任务（HLS/DASH）没有大小：
	// 后台逐个回填。
	go func() {
		for _, t := range mgr.List() {
			if t.Status != engine.StatusDone {
				continue
			}
			if t.Total < 0 {
				mgr.EnsureTotal(t.ID)
			}
			mgr.EnsureDuration(t.ID)
		}
	}()

	go sniffLoop() // 浏览中周期性重扫页面资源
	return nil
}

// Shutdown 在退出前停掉所有任务，保证断点状态落盘。
func Shutdown() {
	if mgr != nil {
		mgr.Shutdown()
	}
	StopMediaServer()
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
