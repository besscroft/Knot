import { useEffect, useState } from "react";
import { Check, Download, Globe, Plus, X } from "lucide-react";
import { useTasks } from "./store/tasks";
import { TaskList, TaskGrid } from "./components/TaskItem";
import { MediaViewer } from "./components/MediaViewer";
import { TaskDetail } from "./components/TaskDetail";
import { RemoveDialog } from "./components/RemoveDialog";
import { NewTaskDialog } from "./components/NewTaskDialog";
import { SettingsView } from "./views/Settings";
import { BrowserView } from "./views/Browser";
import { BrowserToolbar } from "./views/BrowserToolbar";
import { Badge } from "./components/arc/badge/badge";
import { Button } from "./components/arc/button/button";
import { EmptyState } from "./components/arc/empty-state/empty-state";
import SegmentedControl from "./components/arc/segmented-control/segmented-control";
import { IconButton } from "./components/ui";
import { formatSpeed } from "./lib/format";
import { cn } from "./lib/utils";
import { Browser, events } from "./mygo";
import type { BrowserState, Task } from "./mygo";

type View = "active" | "done" | "browser" | "settings";

const VIEWS: { key: View; label: string }[] = [
  { key: "active", label: "下载中" },
  { key: "done", label: "已完成" },
  { key: "browser", label: "浏览器" },
  { key: "settings", label: "设置" },
];

export default function App() {
  const [view, setView] = useState<View>(() => {
    const saved = localStorage.getItem("knot.view");
    return saved === "done" || saved === "browser" || saved === "settings" ? saved : "active";
  });
  const [newDialog, setNewDialog] = useState(false);
  // 已完成页展示方式：卡片（默认，含封面/时长/播放）或列表。
  const [doneView, setDoneView] = useState<"cards" | "list">(() =>
    localStorage.getItem("knot.doneView") === "list" ? "list" : "cards",
  );
  // 正在应用内查看/播放的已完成任务。
  const [playing, setPlaying] = useState<Task | null>(null);
  // 任务详情弹窗：存任务 ID，渲染时从 store 取最新对象（广播自动跟随）。
  const [detailId, setDetailId] = useState<string | null>(null);
  // 删除确认弹窗：询问是否连同磁盘文件一并删除。
  const [removingId, setRemovingId] = useState<string | null>(null);

  function switchDoneView(v: "cards" | "list") {
    setDoneView(v);
    localStorage.setItem("knot.doneView", v);
  }
  const init = useTasks((s) => s.init);
  const tasks = useTasks((s) => s.byId);
  const order = useTasks((s) => s.order);
  const loaded = useTasks((s) => s.loaded);

  useEffect(() => {
    let off: (() => void) | undefined;
    let canceled = false;
    void init().then((unsub) => {
      if (canceled) unsub();
      else off = unsub;
    });
    return () => {
      canceled = true;
      off?.();
    };
  }, [init]);

  const list: Task[] = order.map((id) => tasks[id]);
  // 详情弹窗渲染的是 store 里的最新任务对象，广播更新自动跟随。
  const detailTask = detailId ? (tasks[detailId] ?? null) : null;
  const removingTask = removingId ? (tasks[removingId] ?? null) : null;
  const removeTask = (t: Task) => setRemovingId(t.id);
  const active = list.filter((t) => t.status !== "done");
  const done = list.filter((t) => t.status === "done");
  const running = list.filter((t) => t.status === "running");
  const totalSpeed = running.reduce((sum, t) => sum + t.speed, 0);

  // 浏览状态：会话存活（mode≠app）在侧栏显示状态条；站点窗口可见
  // （mode==browsing）时主区域显示工具栏。切去其他视图 = 暂停会话，
  // 点回「浏览器」= 原样恢复；只有状态条 × 或工具栏「退出」才退出。
  const [browsing, setBrowsing] = useState(false); // 浏览会话存活
  const [browsingActive, setBrowsingActive] = useState(false); // 站点窗口可见
  useEffect(() => {
    const apply = (s: BrowserState) => {
      setBrowsing(s.mode !== "app");
      setBrowsingActive(s.mode === "browsing");
    };
    void Browser.state().then(apply);
    return events.browserState.on(apply);
  }, []);

  function switchView(v: View) {
    if (browsingActive && v !== "browser") void Browser.pause();
    else if (browsing && !browsingActive && v === "browser") void Browser.resume();
    setView(v);
    localStorage.setItem("knot.view", v);
  }

  return (
    <div className="flex h-full">
      {/* 侧栏 */}
      <nav className="flex w-44 shrink-0 flex-col gap-0.5 border-r border-border bg-surface p-2.5">
        <div className="mb-2 flex items-center gap-2 px-2 py-1.5">
          <Download className="size-4 text-accent" />
          <span className="text-sm font-semibold">Knot</span>
        </div>
        {VIEWS.map((v) => {
          const count =
            v.key === "active" ? active.length : v.key === "done" ? done.length : undefined;
          return (
            <button
              key={v.key}
              type="button"
              onClick={() => switchView(v.key)}
              className={cn(
                "flex items-center justify-between rounded-md px-2.5 py-1.5 text-sm transition-colors",
                view === v.key
                  ? "bg-accent-soft font-medium text-accent"
                  : "text-secondary hover:bg-surface-muted hover:text-foreground",
              )}
            >
              <span>{v.label}</span>
              {count !== undefined && count > 0 && (
                <Badge size="sm" pill>
                  {count}
                </Badge>
              )}
            </button>
          );
        })}
        <div className="flex-1" />
        {browsing && (
          <div
            role="button"
            tabIndex={0}
            onClick={() => {
              if (!browsingActive) void Browser.resume();
            }}
            className="mx-0.5 mb-1 flex cursor-pointer items-center justify-between rounded-md border border-accent/40 bg-accent-soft px-2.5 py-1.5 text-xs text-accent transition-colors hover:bg-accent/15"
            title={browsingActive ? "正在浏览" : "点击继续浏览"}
          >
            <span className="flex items-center gap-1.5">
              <Globe className="size-3.5" />
              {browsingActive ? "浏览中" : "浏览已暂停"}
            </span>
            <IconButton
              className="-mr-1 size-5 hover:bg-accent/20 hover:text-accent"
              onClick={(e) => {
                e.stopPropagation();
                void Browser.exit();
              }}
            >
              <X className="size-3.5" />
            </IconButton>
          </div>
        )}
        {running.length > 0 && (
          <div className="px-2.5 py-1.5 text-[11px] text-muted">
            {running.length} 个任务 · {formatSpeed(totalSpeed) || "…"}
          </div>
        )}
      </nav>

      {/* 主区域：浏览会话存活且在"浏览器"视图时显示常驻工具栏
          （乐观渲染，站点子窗口与暂停恢复都靠 browser-state 事件跟进） */}
      <main className="flex min-w-0 flex-1 flex-col">
        {view === "browser" && browsing ? (
          <BrowserToolbar />
        ) : view === "settings" ? (
          <SettingsView />
        ) : view === "browser" ? (
          <BrowserView />
        ) : (
          <>
            <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
              <h1 className="text-sm font-medium">
                {view === "active" ? "下载中" : "已完成"}
              </h1>
              <div className="flex items-center gap-2">
                {view === "done" && (
                  <SegmentedControl
                    label="展示方式"
                    options={[
                      { value: "cards", label: "卡片" },
                      { value: "list", label: "列表" },
                    ]}
                    value={doneView}
                    onValueChange={(v) => switchDoneView(v as "cards" | "list")}
                  />
                )}
                <Button onClick={() => setNewDialog(true)}>
                  <Plus className="size-4" />
                  新建下载
                </Button>
              </div>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              {loaded &&
                (view === "active" ? (
                  <TaskList
                    tasks={active}
                    emptyText="暂无进行中的任务"
                    emptyHint="点击右上角「新建下载」开始"
                    emptyIcon={<Download className="size-10 opacity-40" />}
                    emptyAction={
                      <Button onClick={() => setNewDialog(true)}>
                        <Plus className="size-4" />
                        新建下载
                      </Button>
                    }
                    onDetail={(t) => setDetailId(t.id)}
                    onRemove={removeTask}
                  />
                ) : doneView === "cards" ? (
                  done.length === 0 ? (
                    <EmptyState
                      title="还没有完成的下载"
                      description="完成的任务会归档在这里，可在卡片或列表中回看"
                      icon={<Check className="size-10 opacity-60" />}
                    />
                  ) : (
                    <TaskGrid tasks={done} onPlay={setPlaying} onRemove={removeTask} />
                  )
                ) : (
                  <TaskList
                    tasks={done}
                    emptyText="还没有完成的下载"
                    emptyHint="完成的任务会归档在这里"
                    emptyIcon={<Check className="size-10 opacity-60" />}
                    onDetail={(t) => setDetailId(t.id)}
                    onRemove={removeTask}
                  />
                ))}
            </div>
          </>
        )}
      </main>

      <NewTaskDialog open={newDialog} onClose={() => setNewDialog(false)} />
      {playing && <MediaViewer task={playing} onClose={() => setPlaying(null)} />}
      {removingTask && (
        <RemoveDialog
          key={removingTask.id}
          task={removingTask}
          onClose={() => setRemovingId(null)}
        />
      )}
      {detailTask && (
        <TaskDetail key={detailTask.id} task={detailTask} onClose={() => setDetailId(null)} />
      )}
    </div>
  );
}
