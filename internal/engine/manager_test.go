package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/besscroft/knot/internal/engine/httpdl"
)

// waitFor 轮询直到 pred 为真或超时。
func waitFor(t *testing.T, timeout time.Duration, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待超时")
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	s := NewStore(path)
	if tasks, err := s.Load(); err != nil || tasks != nil {
		t.Fatalf("空存储应返回 (nil,nil)，got (%v,%v)", tasks, err)
	}
	want := []Task{
		{ID: "a", URL: "http://x/a", Name: "a", Status: StatusDone, Total: 10},
		{ID: "b", URL: "http://x/b", Name: "b", Status: StatusPending, Total: -1},
	}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].Status != StatusPending || got[1].Total != -1 {
		t.Fatalf("往返结果不一致: %+v", got)
	}
}

func TestManagerCreateAndFinish(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 64<<10)
	srvURL, done := serveStatic(t, data)
	defer done()

	dir := t.TempDir()
	mgr := NewManager(NewStore(filepath.Join(dir, "tasks.json")), 2)

	var last Task
	mgr.SetNotifier(func(t Task) { last = t })
	mgr.Start()

	created, err := mgr.Create(Task{
		URL:     srvURL,
		Name:    "f.bin",
		Dir:     dir,
		Total:   int64(len(data)),
		Threads: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != StatusPending {
		t.Fatalf("Create 返回异常: %+v", created)
	}

	waitFor(t, 10*time.Second, func() bool {
		task, _ := mgr.Get(created.ID)
		return task.Status == StatusDone
	})
	if last.ID != created.ID || last.Status != StatusDone {
		t.Fatalf("通知的最终快照异常: %+v", last)
	}

	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("下载内容不一致: err=%v len=%d", err, len(got))
	}
	if _, err := os.Stat(httpdl.SidecarPath(filepath.Join(dir, "f.bin"))); !os.IsNotExist(err) {
		t.Fatalf("完成后 sidecar 应删除")
	}
}

func TestManagerPauseResumeAndRemove(t *testing.T) {
	data := bytes.Repeat([]byte{0xCD}, 64<<10)
	srvURL, done := serveStatic(t, data)
	defer done()

	dir := t.TempDir()
	mgr := NewManager(NewStore(filepath.Join(dir, "tasks.json")), 2)
	mgr.Start()

	created, err := mgr.Create(Task{URL: srvURL, Name: "p.bin", Dir: dir, Total: int64(len(data)), Threads: 4})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, 5*time.Second, func() bool {
		task, _ := mgr.Get(created.ID)
		return task.Status == StatusRunning
	})
	if err := mgr.Pause(created.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		task, _ := mgr.Get(created.ID)
		return task.Status == StatusPaused
	})

	if err := mgr.Resume(created.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, func() bool {
		task, _ := mgr.Get(created.ID)
		return task.Status == StatusDone
	})

	if err := mgr.Remove(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := mgr.Get(created.ID); ok {
		t.Fatal("Remove 后任务应不存在")
	}
}

func TestManagerRestoreConvertsRunningToPaused(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "tasks.json"))
	if err := store.Save([]Task{
		{ID: "x", URL: "http://x/x", Name: "x.bin", Dir: dir, Status: StatusRunning, Total: 10},
		{ID: "y", URL: "http://x/y", Name: "y.bin", Dir: dir, Status: StatusDone, Total: 10},
	}); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store, 2)
	mgr.Start()
	waitFor(t, time.Second, func() bool { return len(mgr.List()) == 2 })

	x, _ := mgr.Get("x")
	if x.Status != StatusPaused {
		t.Fatalf("上次会话的 running 应恢复为 paused，got %s", x.Status)
	}
	y, _ := mgr.Get("y")
	if y.Status != StatusDone {
		t.Fatalf("done 状态应保持，got %s", y.Status)
	}
}
