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
import { Check, FileText, Folder, Music, Pause, Play, Trash2 } from "lucide-react";
import { Badge } from "./arc/badge/badge";
import { Card } from "./arc/card/card";
import { EmptyState } from "./arc/empty-state/empty-state";
import { Progress } from "./arc/progress/progress";
import { Tooltip } from "./arc/tooltip/tooltip";
import { IconButton, ProgressIndeterminate } from "./ui";

export const STATUS_LABEL: Record<Task["status"], string> = {
  pending: "等待中",
  running: "下载中",
  paused: "已暂停",
  done: "已完成",
  error: "出错",
};

export function statusTone(status: Task["status"]) {
  switch (status) {
    case "running":
      return "info" as const;
    case "done":
      return "success" as const;
    case "error":
      return "danger" as const;
    default:
      return "neutral" as const;
  }
}

/** kind 徽章：hls / dash 显示对应标签，直链任务不显示。 */
export function kindBadge(kind?: string) {
  if (kind === "hls")
    return (
      <Badge size="sm" color="violet">
        HLS
      </Badge>
    );
  if (kind === "dash")
    return (
      <Badge size="sm" color="sky">
        DASH
      </Badge>
    );
  return null;
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
  const indeterminate = running && !isHLS && task.total < 0;

  return (
    <div className="group flex items-center gap-3 border-b border-border px-4 py-3 transition-colors last:border-b-0 hover:bg-surface-muted/60">
      <div
        className={cn("flex min-w-0 flex-1 flex-col gap-1.5", onDetail && "cursor-pointer")}
        onClick={() => onDetail?.(task)}
      >
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{task.name}</span>
          {kindBadge(task.kind)}
          <Badge size="sm" tone={statusTone(task.status)}>
            {STATUS_LABEL[task.status]}
          </Badge>
        </div>
        {indeterminate ? (
          <ProgressIndeterminate className="opacity-90" />
        ) : (
          <Progress value={fraction * 100} className="opacity-90" />
        )}
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
          <Tooltip content="暂停">
            <IconButton onClick={() => void Tasks.pause(task.id)}>
              <Pause className="size-4" />
            </IconButton>
          </Tooltip>
        ) : task.status === "done" ? (
          <Tooltip content="打开文件">
            <IconButton onClick={() => void Tasks.openPath(task.id)}>
              <Check className="size-4" />
            </IconButton>
          </Tooltip>
        ) : (
          <Tooltip content={task.status === "pending" ? "开始" : "继续"}>
            <IconButton onClick={() => void Tasks.resume(task.id)}>
              <Play className="size-4" />
            </IconButton>
          </Tooltip>
        )}
        <Tooltip content="在文件夹中显示">
          <IconButton onClick={() => void Tasks.reveal(task.id)}>
            <Folder className="size-4" />
          </IconButton>
        </Tooltip>
        <Tooltip content="删除任务">
          <IconButton className="hover:text-danger" onClick={() => onRemove(task)}>
            <Trash2 className="size-4" />
          </IconButton>
        </Tooltip>
      </div>
    </div>
  );
}

/** TaskList：给定过滤条件的任务列表（含空态与可选的空态动作）。 */
export function TaskList({
  tasks,
  emptyText,
  emptyHint,
  emptyIcon,
  emptyAction,
  onDetail,
  onRemove,
}: {
  tasks: Task[];
  emptyText: string;
  emptyHint?: string;
  emptyIcon?: ReactNode;
  emptyAction?: ReactNode;
  onDetail?: (t: Task) => void;
  onRemove: (t: Task) => void;
}) {
  if (tasks.length === 0) {
    return (
      <EmptyState
        title={emptyText}
        description={emptyHint ?? "列表为空"}
        icon={emptyIcon}
        action={emptyAction}
      />
    );
  }
  return (
    <div className="rounded-lg border border-border bg-surface">
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
    <div className="grid grid-cols-4 gap-3">
      {tasks.map((t) => (
        <TaskCard key={t.id} task={t} onPlay={onPlay} onRemove={onRemove} />
      ))}
    </div>
  );
}

/** TaskCard：单个已完成任务的媒体卡片（Arc Card：自带悬停浮起与封面微缩放）。 */
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
    <Card
      className="group task-media-card"
      title={task.name}
      media={
        <div className="relative w-full">
          {/* 封面区：图片直出；视频缩略图（404 时露出图标）；音频/其他用图标 */}
          <button
            type="button"
            onClick={() => (playable || kind === "image") && onPlay(task)}
            className="relative block aspect-video w-full cursor-pointer"
            title={playable || kind === "image" ? "点击播放/查看" : task.name}
          >
            {kind !== "image" && (
              <div className="absolute inset-0 flex items-center justify-center text-muted/50">
                {kind === "audio" ? <Music className="size-9" /> : <FileText className="size-9" />}
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
                  <Play className="size-5" />
                </span>
              </span>
            )}
            {kind !== "other" && (task.duration ?? 0) > 0 && (
              <span className="absolute bottom-1.5 right-1.5 rounded bg-black/70 px-1 py-0.5 text-[10px] font-medium text-white">
                {formatDuration(task.duration ?? 0)}
              </span>
            )}
          </button>

          {/* 悬停操作：浮在封面右上角，不占下方信息行的空间 */}
          <div className="absolute right-2 top-2 flex items-center gap-0.5 rounded-full bg-black/55 p-0.5 opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            <Tooltip content="打开文件">
              <IconButton
                className="size-6 text-white/85 hover:bg-white/20! hover:text-white!"
                onClick={() => void Tasks.openPath(task.id)}
              >
                <Check className="size-3.5" />
              </IconButton>
            </Tooltip>
            <Tooltip content="在文件夹中显示">
              <IconButton
                className="size-6 text-white/85 hover:bg-white/20! hover:text-white!"
                onClick={() => void Tasks.reveal(task.id)}
              >
                <Folder className="size-3.5" />
              </IconButton>
            </Tooltip>
            <Tooltip content="删除任务">
              <IconButton
                className="size-6 text-white/85 hover:bg-white/20! hover:text-white!"
                onClick={() => onRemove(task)}
              >
                <Trash2 className="size-3.5" />
              </IconButton>
            </Tooltip>
          </div>
        </div>
      }
    >
      {/* 信息行：紧凑自排，不走 Card footer（其默认间距对 4 列小卡片太松） */}
      <p
        className="mt-1.5 truncate text-xs text-muted"
        title={`${formatBytes(task.total)}${when ? ` · ${when}` : ""}`}
      >
        {formatBytes(task.total)}
        {when && ` · ${when}`}
      </p>
    </Card>
  );
}
