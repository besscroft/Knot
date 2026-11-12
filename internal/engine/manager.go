package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/besscroft/knot/internal/engine/hls"
	"github.com/besscroft/knot/internal/engine/httpdl"
)

// Notifier 在任务变化（状态流转或周期性进度）时收到任务快照。
type Notifier func(Task)

// Manager 管理任务队列：并发上限、状态流转、持久化与进度通知。
// 所有方法并发安全。
type Manager struct {
	store  *Store
	notify Notifier // 可为 nil

	mu     sync.Mutex
	maxRun int
	tasks  map[string]*entry
	order  []string // 创建顺序，遍历与持久化都用它

	probing map[string]bool // 正在探测时长的任务 ID（去重）

	runWG sync.WaitGroup // 进行中的 runTask goroutine
}

type entry struct {
	task     Task
	cancel   context.CancelFunc
	done     chan struct{} // 关闭表示 runTask 已退出
	pausing  bool
	removing bool
	last     int64         // 上次采样的 Downloaded
	speedEMA float64       // 平滑后的速度
	shards   []ShardDetail // 最近一次分片快照（http 多线程任务；暂停后保留供展示）
}

// NewManager 创建队列管理器；maxRun 为同时进行的任务数上限。
func NewManager(store *Store, maxRun int) *Manager {
	if maxRun < 1 {
		maxRun = 1
	}
	return &Manager{
		store:   store,
		maxRun:  maxRun,
		tasks:   make(map[string]*entry),
		probing: make(map[string]bool),
	}
}

// SetNotifier 设置任务通知回调；须在 Start 之前调用。
func (m *Manager) SetNotifier(n Notifier) { m.notify = n }

// Start 恢复持久化的任务（上次会话中断的 running 一律转为 paused）并启动进度循环。
func (m *Manager) Start() {
	tasks, err := m.store.Load()
	if err != nil {
		// 任务文件损坏：改名保留原件再从空列表开始，避免空列表覆盖掉
		// 可能可抢救的数据；改名失败（如被占用）也只能照常继续。
		_ = m.store.Backup()
		tasks = nil
	}
	m.mu.Lock()
	for _, t := range tasks {
		if t.Status == StatusRunning {
			t.Status = StatusPaused
			t.Speed = 0
		}
		e := &entry{task: t, done: make(chan struct{})}
		m.tasks[t.ID] = e
		m.order = append(m.order, t.ID)
	}
	m.mu.Unlock()

	go m.tickLoop()
	m.persist()
	m.pump()
}

// Create 登记一个新任务（service 层负责探测与命名），返回带 ID 的任务。
func (m *Manager) Create(t Task) (Task, error) {
	if t.URL == "" {
		return Task{}, errors.New("URL 不能为空")
	}
	m.mu.Lock()
	t.ID = newID()
	t.Status = StatusPending
	t.CreatedAt = time.Now()
	e := &entry{task: t, done: make(chan struct{})}
	m.tasks[t.ID] = e
	m.order = append(m.order, t.ID)
	created := e.task
	m.mu.Unlock()

	m.persist()
	m.fire(created)
	m.pump()
	return created, nil
}

// Pause 暂停任务：running 会等 goroutine 退出，pending 直接转 paused。
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	e, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}
	switch e.task.Status {
	case StatusRunning:
		e.pausing = true
		cancel, done := e.cancel, e.done
		m.mu.Unlock()
		cancel()
		<-done // runTask 退出前已把状态置为 paused 并持久化
		return nil
	case StatusPending:
		e.task.Status = StatusPaused
		snapshot := e.task
		m.mu.Unlock()
		m.persist()
		m.fire(snapshot)
		return nil
	default:
		m.mu.Unlock()
		return fmt.Errorf("任务 %s 当前不可暂停", id)
	}
}

// Resume 恢复 paused/error 的任务为 pending 并尝试启动。
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	e, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}
	if e.task.Status != StatusPaused && e.task.Status != StatusError {
		m.mu.Unlock()
		return fmt.Errorf("任务 %s 当前不可恢复", id)
	}
	e.task.Status = StatusPending
	e.task.Error = ""
	e.task.Speed = 0
	snapshot := e.task
	m.mu.Unlock()

	m.persist()
	m.fire(snapshot)
	m.pump()
	return nil
}

// Remove 删除任务记录（若在运行则先停止），并清理 sidecar；不删除已下载的文件。
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	e, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}
	running := e.task.Status == StatusRunning
	e.removing = true
	cancel, done := e.cancel, e.done
	m.mu.Unlock()

	if running {
		cancel()
		<-done
	}

	m.mu.Lock()
	delete(m.tasks, id)
	m.order = slices.DeleteFunc(m.order, func(s string) bool { return s == id })
	m.mu.Unlock()

	httpdl.RemoveState(e.task.Path())
	if e.task.Kind == "hls" {
		_ = os.RemoveAll(taskWorkDir(e.task))
	}
	if e.task.Kind == "dash" {
		_ = os.RemoveAll(dashWorkDir(e.task))
	}
	m.persist()
	return nil
}

// List 按创建顺序返回全部任务的快照。
func (m *Manager) List() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.tasks[id].task)
	}
	return out
}

// Get 返回单个任务快照。
func (m *Manager) Get(id string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.tasks[id]
	if !ok {
		return Task{}, false
	}
	return e.task, true
}

// Shards 返回任务最近的分片快照（副本）。仅 http 多线程任务有数据；
// 暂停/出错后保留最后一次快照供详情展示。任务不存在返回 nil。
func (m *Manager) Shards(id string) []ShardDetail {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.tasks[id]
	if !ok || len(e.shards) == 0 {
		return nil
	}
	out := make([]ShardDetail, len(e.shards))
	copy(out, e.shards)
	return out
}

// SetMaxRun 更新同时进行的任务数上限，并按需补启动。
func (m *Manager) SetMaxRun(n int) {
	if n < 1 {
		n = 1
	}
	m.mu.Lock()
	m.maxRun = n
	m.mu.Unlock()
	m.pump()
}

// Shutdown 停止所有运行中的任务并等待退出（应用退出前调用）。
func (m *Manager) Shutdown() {
	m.mu.Lock()
	var cancels []context.CancelFunc
	for _, e := range m.tasks {
		if e.task.Status == StatusRunning {
			e.pausing = true
			cancels = append(cancels, e.cancel)
		}
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	m.runWG.Wait()
}

// pump 在锁外补启动可运行的 pending 任务。
func (m *Manager) pump() {
	for {
		m.mu.Lock()
		running := 0
		var next *entry
		for _, id := range m.order {
			e := m.tasks[id]
			switch e.task.Status {
			case StatusRunning:
				running++
			case StatusPending:
				if next == nil {
					next = e
				}
			}
		}
		if next == nil || running >= m.maxRun {
			m.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		next.task.Status = StatusRunning
		next.task.Error = ""
		next.cancel = cancel
		next.last = next.task.Downloaded
		next.speedEMA = 0
		next.done = make(chan struct{}) // 每次运行用新的 done，供本轮等待者关闭
		m.runWG.Add(1)
		m.mu.Unlock()

		go m.runTask(ctx, next)
	}
}

// runTask 执行一次下载，结束时按结果落状态。
func (m *Manager) runTask(ctx context.Context, e *entry) {
	defer m.runWG.Done()
	defer close(e.done)

	m.mu.Lock()
	t := e.task
	m.mu.Unlock()

	var err error
	headers := taskHeaders(t)
	switch t.Kind {
	case "hls":
		err = hls.Download(ctx, t.URL, taskWorkDir(t), t.Path(), hls.Options{
			Threads: t.Threads,
			Headers: headers,
			OnProgress: func(downloaded int64, segDone, segTotal int) {
				m.mu.Lock()
				e.task.Downloaded = downloaded
				e.task.SegDone = int64(segDone)
				e.task.SegTotal = int64(segTotal)
				m.mu.Unlock()
			},
		})
	case "dash":
		err = m.runDash(ctx, e, t)
	default:
		err = httpdl.Download(ctx, t.URL, t.Path(), httpdl.Options{
			Threads: t.Threads,
			Headers: headers,
			OnProgress: func(downloaded, _ int64) {
				m.mu.Lock()
				e.task.Downloaded = downloaded
				m.mu.Unlock()
			},
			OnShards: func(segs []httpdl.Segment) {
				shards := make([]ShardDetail, len(segs))
				for i, s := range segs {
					shards[i] = ShardDetail{
						Index: i, Start: s.Start, End: s.End, Written: s.Written,
						Active: s.Active, Retrying: s.Retrying,
					}
				}
				m.mu.Lock()
				e.shards = shards
				m.mu.Unlock()
			},
		})
	}

	m.mu.Lock()
	switch {
	case e.removing:
		// Remove 负责删记录，这里只退出。
	case errors.Is(err, context.Canceled) && e.pausing:
		e.task.Status = StatusPaused
		e.task.Speed = 0
	case err != nil:
		e.task.Status = StatusError
		e.task.Error = err.Error()
		e.task.Speed = 0
	default:
		// 目标格式转换（如 mkv/mp3）：失败则任务置错并保留原文件。
		if _, cerr := Finalize(ctx, t.Path(), t.Format); cerr != nil {
			if ctx.Err() != nil {
				e.task.Status = StatusPaused
				e.task.Speed = 0
			} else {
				e.task.Status = StatusError
				e.task.Error = cerr.Error()
				e.task.Speed = 0
			}
			snapshot := e.task
			m.mu.Unlock()
			m.persist()
			m.fire(snapshot)
			m.pump()
			return
		}
		now := time.Now()
		e.task.Status = StatusDone
		e.task.CompletedAt = &now
		e.task.Speed = 0
		if total := e.task.Total; total >= 0 {
			e.task.Downloaded = total
		} else if info, err := os.Stat(e.task.Path()); err == nil && info.Size() > 0 {
			// HLS/DASH 等流式任务下载时无 content-length：完成后按落盘文件回填真实大小
			e.task.Total = info.Size()
			e.task.Downloaded = info.Size()
		}
		if t.Kind == "hls" {
			e.task.SegDone = e.task.SegTotal // HLS 完成时分片数对齐
		}
	}
	e.pausing = false
	snapshot := e.task
	m.mu.Unlock()

	m.persist()
	m.fire(snapshot)
	m.pump()
	if snapshot.Status == StatusDone {
		go m.EnsureDuration(snapshot.ID) // 后台探测媒体时长，不阻塞队列
	}
}

// EnsureDuration 确保 done 媒体任务的时长已探测并写回（任务完成与启动
// 回填共用）。探测在锁外进行，probing 集合防止并发重复探测。
func (m *Manager) EnsureDuration(id string) {
	m.mu.Lock()
	e, ok := m.tasks[id]
	if !ok || e.task.Status != StatusDone || e.task.Duration > 0 {
		m.mu.Unlock()
		return
	}
	kind := MediaKind(e.task.Name)
	if kind != "video" && kind != "audio" {
		m.mu.Unlock()
		return
	}
	if m.probing[id] {
		m.mu.Unlock()
		return
	}
	m.probing[id] = true
	path := e.task.Path()
	m.mu.Unlock()

	d := ProbeDuration(context.Background(), path)

	m.mu.Lock()
	delete(m.probing, id)
	e, ok = m.tasks[id]
	if !ok || d <= 0 || e.task.Duration > 0 {
		m.mu.Unlock()
		return
	}
	e.task.Duration = d
	snapshot := e.task
	m.mu.Unlock()

	m.persist()
	m.fire(snapshot)
}

// EnsureTotal 确保 done 任务的展示大小与磁盘文件一致（HLS/DASH 等流式任务
// 下载时无 content-length，total=-1，完成后按落盘文件回填；任务完成时与
// 启动回填共用）。文件不存在时不回填（可能已被外部删除）。
func (m *Manager) EnsureTotal(id string) {
	m.mu.Lock()
	e, ok := m.tasks[id]
	if !ok || e.task.Status != StatusDone || e.task.Total >= 0 {
		m.mu.Unlock()
		return
	}
	path := e.task.Path()
	m.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 {
		return
	}

	m.mu.Lock()
	e, ok = m.tasks[id]
	if !ok || e.task.Status != StatusDone || e.task.Total >= 0 {
		m.mu.Unlock()
		return
	}
	e.task.Total = info.Size()
	e.task.Downloaded = info.Size()
	snapshot := e.task
	m.mu.Unlock()

	m.persist()
	m.fire(snapshot)
}

// taskWorkDir 返回任务的工作目录（HLS 分片缓存）。
func taskWorkDir(t Task) string { return filepath.Join(t.Dir, ".knot-hls-"+t.ID) }

// taskHeaders 从任务字段构建上下文请求头（无则 nil）。
func taskHeaders(t Task) *httpdl.Headers {
	if t.Referer == "" && t.Cookie == "" && t.UserAgent == "" {
		return nil
	}
	return &httpdl.Headers{Referer: t.Referer, Cookie: t.Cookie, UserAgent: t.UserAgent}
}

// tickLoop 每秒计算运行中任务的速率并通知。
func (m *Manager) tickLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		m.mu.Lock()
		var changed []Task
		for _, id := range m.order {
			e := m.tasks[id]
			if e.task.Status != StatusRunning {
				continue
			}
			inst := e.task.Downloaded - e.last
			if inst < 0 {
				inst = 0
			}
			e.last = e.task.Downloaded
			e.speedEMA = 0.5*float64(inst) + 0.5*e.speedEMA
			e.task.Speed = int64(e.speedEMA)
			changed = append(changed, e.task)
		}
		m.mu.Unlock()

		if m.notify != nil {
			for _, task := range changed {
				m.notify(task)
			}
		}
	}
}

// persist 把当前任务列表写入磁盘。
func (m *Manager) persist() {
	tasks := m.List()
	if err := m.store.Save(tasks); err != nil {
		// 持久化失败不致命，忽略（内存态仍是权威）。
		_ = err
	}
}

// fire 发送任务快照（须传入锁内取出的副本；回调自身不得再进入 Manager）。
func (m *Manager) fire(t Task) {
	if m.notify != nil {
		m.notify(t)
	}
}

// newID 生成 12 位十六进制随机 ID。
func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败几乎不可能，退化为时间戳。
		return strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
	}
	return hex.EncodeToString(b[:])
}
