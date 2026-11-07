import type { ReactNode } from "react";
import { Tasks, type Task } from "../mygo";
import {
  formatBytes,
  formatDuration,
  formatProgress,
  formatSpeed,
  mediaFileUrl,
  mediaKindOf,
  mediaThumbUrl,
} from "../lib/format";
import { cn } from "../lib/utils";
import {
  IconCheck,
  IconFile,
  IconFolder,
  IconMusic,
  IconPause,
  IconPlay,
  IconTrash,
} from "./icons";
import { EmptyState, IconButton, Progress } from "./ui";

export const STATUS_LABEL: Record<Task["status"], string> = {
  pending: "等待中",
  running: "下载中",
  paused: "已暂停",
  done: "已完成",
  error: "出错",
};

export function statusColor(status: Task["status"]) {
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

/** TaskItem：一行任务的完整展示与操作（点击主体打开详情）。 */
export function TaskItem({
  task,
  onDetail,
  onRemove,
}: {
  task: Task;
  onDetail?: (t: Task) => void;
  onRemove: (t: Task) => void;
}) {
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
      <div
        className={cn("flex min-w-0 flex-1 flex-col gap-1.5", onDetail && "cursor-pointer")}
        onClick={() => onDetail?.(task)}
      >
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
          onClick={() => onRemove(task)}
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
  onDetail,
  onRemove,
}: {
  tasks: Task[];
  emptyText: string;
  emptyHint?: string;
  emptyIcon?: ReactNode;
  onDetail?: (t: Task) => void;
  onRemove: (t: Task) => void;
}) {
  if (tasks.length === 0) {
    return <EmptyState icon={emptyIcon} text={emptyText} hint={emptyHint} />;
  }
  return (
    <div className="rounded-lg border border-border bg-panel">
      {tasks.map((t) => (
        <TaskItem key={t.id} task={t} onDetail={onDetail} onRemove={onRemove} />
      ))}
    </div>
  );
}

/** TaskGrid：已完成任务的卡片网格（封面/时长/点击播放）。 */
export function TaskGrid({
  tasks,
  onPlay,
  onRemove,
}: {
  tasks: Task[];
  onPlay: (t: Task) => void;
  onRemove: (t: Task) => void;
}) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-3">
      {tasks.map((t) => (
        <TaskCard key={t.id} task={t} onPlay={onPlay} onRemove={onRemove} />
      ))}
    </div>
  );
}

/** TaskCard：单个已完成任务的媒体卡片。 */
export function TaskCard({
  task,
  onPlay,
  onRemove,
}: {
  task: Task;
  onPlay: (t: Task) => void;
  onRemove: (t: Task) => void;
}) {
  const kind = mediaKindOf(task.name);
  const playable = kind === "video" || kind === "audio";
  const when = task.completedAt
    ? new Date(task.completedAt).toLocaleString([], {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "";

  return (
    <div className="group overflow-hidden rounded-lg border border-border bg-panel transition-colors hover:border-accent/50">
      {/* 封面区：图片直出；视频缩略图（404 时露出图标）；音频/其他用图标 */}
      <button
        type="button"
        onClick={() => (playable || kind === "image") && onPlay(task)}
        className="relative block aspect-video w-full cursor-pointer bg-panel-2/60"
        title={playable || kind === "image" ? "点击播放/查看" : task.name}
      >
        {kind !== "image" && (
          <div className="flex h-full items-center justify-center text-muted/50">
            {kind === "audio" ? <IconMusic className="size-9" /> : <IconFile className="size-9" />}
          </div>
        )}
        {(kind === "image" || kind === "video") && (
          <img
            src={kind === "image" ? mediaFileUrl(task.id) : mediaThumbUrl(task.id)}
            loading="lazy"
            alt={task.name}
            className="absolute inset-0 h-full w-full object-cover"
            onError={(e) => {
              e.currentTarget.style.display = "none";
            }}
          />
        )}
        {playable && (
          <span className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity group-hover:opacity-100">
            <span className="flex size-10 items-center justify-center rounded-full bg-black/60 text-white">
              <IconPlay className="size-5" />
            </span>
          </span>
        )}
        {kind !== "other" && (task.duration ?? 0) > 0 && (
          <span className="absolute bottom-1.5 right-1.5 rounded bg-black/70 px-1 py-0.5 text-[10px] font-medium text-white">
            {formatDuration(task.duration ?? 0)}
          </span>
        )}
      </button>

      {/* 信息与操作 */}
      <div className="p-2.5">
        <div className="truncate text-xs font-medium" title={task.name}>
          {task.name}
        </div>
        <div className="mt-1.5 flex items-center justify-between gap-1">
          <span className="min-w-0 truncate text-[11px] text-muted">
            {formatBytes(task.total)}
            {when && ` · ${when}`}
          </span>
          <div className="flex shrink-0 items-center gap-0 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            <IconButton title="打开文件" className="size-6" onClick={() => void Tasks.openPath(task.id)}>
              <IconCheck className="size-3.5" />
            </IconButton>
            <IconButton title="在文件夹中显示" className="size-6" onClick={() => void Tasks.reveal(task.id)}>
              <IconFolder className="size-3.5" />
            </IconButton>
            <IconButton title="删除任务" className="size-6 hover:text-danger" onClick={() => onRemove(task)}>
              <IconTrash className="size-3.5" />
            </IconButton>
          </div>
        </div>
      </div>
    </div>
  );
}
