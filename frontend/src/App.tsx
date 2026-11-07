import { useEffect, useState } from "react";
import { useTasks } from "./store/tasks";
import { TaskList } from "./components/TaskItem";
import { NewTaskDialog } from "./components/NewTaskDialog";
import { SettingsView } from "./views/Settings";
import { BrowserView } from "./views/Browser";
import { Button } from "./components/ui";
import { IconCheck, IconDownload, IconGlobe, IconPlus, IconX } from "./components/icons";
import { formatSpeed } from "./lib/format";
import { cn } from "./lib/utils";
import { Browser, events } from "./mygo";
import type { Task } from "./mygo";

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
  const active = list.filter((t) => t.status !== "done");
  const done = list.filter((t) => t.status === "done");
  const running = list.filter((t) => t.status === "running");
  const totalSpeed = running.reduce((sum, t) => sum + t.speed, 0);

  // 浏览状态：浏览中在侧栏显示状态条，切换视图即退出浏览。
  const [browsing, setBrowsing] = useState(false);
  useEffect(() => {
    void Browser.state().then((s) => setBrowsing(s.mode === "browsing"));
    return events.browserState.on((s) => setBrowsing(s.mode === "browsing"));
  }, []);

  function switchView(v: View) {
    if (browsing) void Browser.exit();
    setView(v);
    localStorage.setItem("knot.view", v);
  }

  return (
    <div className="flex h-full">
      {/* 侧栏 */}
      <nav className="flex w-44 shrink-0 flex-col gap-0.5 border-r border-border bg-panel p-2.5">
        <div className="mb-2 flex items-center gap-2 px-2 py-1.5">
          <IconDownload className="size-4 text-accent" />
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
                  : "text-text hover:bg-panel-2",
              )}
            >
              <span>{v.label}</span>
              {count !== undefined && count > 0 && (
                <span className="rounded-full bg-panel-2 px-1.5 text-[11px] text-muted">
                  {count}
                </span>
              )}
            </button>
          );
        })}
        <div className="flex-1" />
        {browsing && (
          <button
            type="button"
            onClick={() => void Browser.exit()}
            className="mx-0.5 mb-1 flex items-center justify-between rounded-md border border-accent/40 bg-accent-soft px-2.5 py-1.5 text-xs text-accent transition-colors hover:bg-accent/15"
            title="退出浏览，回到应用界面"
          >
            <span className="flex items-center gap-1.5">
              <IconGlobe className="size-3.5" />
              浏览中
            </span>
            <IconX className="size-3.5" />
          </button>
        )}
        {running.length > 0 && (
          <div className="px-2.5 py-1.5 text-[11px] text-muted">
            {running.length} 个任务 · {formatSpeed(totalSpeed) || "…"}
          </div>
        )}
      </nav>

      {/* 主区域 */}
      <main className="flex min-w-0 flex-1 flex-col">
        {view === "settings" ? (
          <SettingsView />
        ) : view === "browser" ? (
          <BrowserView />
        ) : (
          <>
            <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
              <h1 className="text-sm font-medium">
                {view === "active" ? "下载中" : "已完成"}
              </h1>
              <Button onClick={() => setNewDialog(true)}>
                <IconPlus className="size-4" />
                新建下载
              </Button>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              {loaded &&
                (view === "active" ? (
                  <TaskList
                    tasks={active}
                    emptyText="暂无进行中的任务"
                    emptyHint="点击右上角「新建下载」开始"
                    emptyIcon={<IconDownload className="size-10 opacity-40" />}
                  />
                ) : (
                  <TaskList
                    tasks={done}
                    emptyText="还没有完成的下载"
                    emptyIcon={<IconCheck className="size-10 opacity-40" />}
                  />
                ))}
            </div>
          </>
        )}
      </main>

      <NewTaskDialog open={newDialog} onClose={() => setNewDialog(false)} />
    </div>
  );
}
