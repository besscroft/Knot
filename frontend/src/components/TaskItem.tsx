import type { ReactNode } from "react";
import { Tasks, type Task } from "../mygo";
import { formatProgress, formatSpeed } from "../lib/format";
import { cn } from "../lib/utils";
import { useTasks } from "../store/tasks";
import {
  IconCheck,
  IconFolder,
  IconPause,
  IconPlay,
  IconTrash,
} from "./icons";
import { EmptyState, IconButton, Progress } from "./ui";

const STATUS_LABEL: Record<Task["status"], string> = {
  pending: "等待中",
  running: "下载中",
  paused: "已暂停",
  done: "已完成",
  error: "出错",
};

function statusColor(status: Task["status"]) {
  switch (status) {
    case "running":
      return "text-accent bg-accent-soft";
    case "done":
      return "text-success bg-success/10";
    case "error":
      return "text-danger bg-danger/10";
    default:
      return "text-muted bg-panel-2";
  }
}

/** TaskItem：一行任务的完整展示与操作。 */
export function TaskItem({ task }: { task: Task }) {
  const removeLocal = useTasks((s) => s.removeLocal);
  const running = task.status === "running";
  const isHLS = task.kind === "hls";
  const segFraction =
    task.segTotal && task.segTotal > 0 ? (task.segDone ?? 0) / task.segTotal : 0;
  const fraction = isHLS
    ? segFraction
    : task.total >= 0
      ? Math.min(1, task.downloaded / task.total)
      : 0;

  return (
    <div className="group flex items-center gap-3 border-b border-border px-4 py-3 transition-colors last:border-b-0 hover:bg-panel-2/60">
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{task.name}</span>
          {isHLS && (
            <span className="shrink-0 rounded bg-violet-500/10 px-1.5 py-0.5 text-[11px] font-medium text-violet-500">
              HLS
            </span>
          )}
          <span
            className={cn(
              "shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium",
              statusColor(task.status),
            )}
          >
            {STATUS_LABEL[task.status]}
          </span>
        </div>
        <Progress
          value={fraction}
          indeterminate={running && !isHLS && task.total < 0}
          className="opacity-90"
        />
        <div className="flex items-center gap-2 text-xs text-muted">
          {isHLS ? (
            <span>
              分片 {task.segDone ?? 0}/{task.segTotal ?? 0}
            </span>
          ) : (
            <span>{formatProgress(task.downloaded, task.total)}</span>
          )}
          {task.speed > 0 && <span className="text-accent">{formatSpeed(task.speed)}</span>}
          {fraction > 0 && fraction < 1 && <span>{(fraction * 100).toFixed(1)}%</span>}
          {task.status === "error" && task.error && (
            <span className="truncate text-danger" title={task.error}>
              {task.error}
            </span>
          )}
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
        {task.status === "running" ? (
          <IconButton title="暂停" onClick={() => void Tasks.pause(task.id)}>
            <IconPause className="size-4" />
          </IconButton>
        ) : task.status === "done" ? (
          <IconButton title="打开文件" onClick={() => void Tasks.openPath(task.id)}>
            <IconCheck className="size-4" />
          </IconButton>
        ) : (
          <IconButton title={task.status === "pending" ? "开始" : "继续"} onClick={() => void Tasks.resume(task.id)}>
            <IconPlay className="size-4" />
          </IconButton>
        )}
        <IconButton title="在文件夹中显示" onClick={() => void Tasks.reveal(task.id)}>
          <IconFolder className="size-4" />
        </IconButton>
        <IconButton
          title="删除任务"
          className="hover:text-danger"
          onClick={() => {
            void Tasks.remove(task.id).then(() => removeLocal(task.id));
          }}
        >
          <IconTrash className="size-4" />
        </IconButton>
      </div>
    </div>
  );
}

/** TaskList：给定过滤条件的任务列表（含空态）。 */
export function TaskList({
  tasks,
  emptyText,
  emptyHint,
  emptyIcon,
}: {
  tasks: Task[];
  emptyText: string;
  emptyHint?: string;
  emptyIcon?: ReactNode;
}) {
  if (tasks.length === 0) {
    return <EmptyState icon={emptyIcon} text={emptyText} hint={emptyHint} />;
  }
  return (
    <div className="rounded-lg border border-border bg-panel">
      {tasks.map((t) => (
        <TaskItem key={t.id} task={t} />
      ))}
    </div>
  );
}
