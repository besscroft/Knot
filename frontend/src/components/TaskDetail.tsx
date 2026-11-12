import { useEffect, useRef, useState } from "react";
import { Tasks, type ShardDetail, type Task } from "../mygo";
import { formatBytes, formatDuration, formatProgress, formatSpeed } from "../lib/format";
import { cn } from "../lib/utils";
import { Badge } from "./arc/badge/badge";
import { Button } from "./arc/button/button";
import { Dialog, DialogContent } from "./arc/dialog/dialog";
import { Progress } from "./arc/progress/progress";
import { ProgressIndeterminate } from "./ui";
import { STATUS_LABEL, statusTone } from "./TaskItem";

type ShardView = ShardDetail & { speed: number };

/**
 * TaskDetail：任务详情弹窗。http 多线程任务展示每个分片的实时进度与
 * 速度（每秒轮询 Tasks.shards，速度由两次轮询的字节差值算出）；
 * HLS 任务展示分片聚合计数；暂停/出错展示最后一次快照（冻结）。
 */
export function TaskDetail({ task, onClose }: { task: Task; onClose: () => void }) {
  const [shards, setShards] = useState<ShardView[]>([]);
  const [stale, setStale] = useState(false); // 快照是否来自暂停前的冻结状态
  const prev = useRef<{ t: number; w: number[] }>({ t: 0, w: [] });
  const running = task.status === "running";
  const isHLS = task.kind === "hls";
  const fraction = isHLS
    ? task.segTotal && task.segTotal > 0
      ? (task.segDone ?? 0) / task.segTotal
      : 0
    : task.total >= 0
      ? Math.min(1, task.downloaded / task.total)
      : 0;
  const remaining = task.total >= 0 ? Math.max(0, task.total - task.downloaded) : -1;
  const eta = running && task.speed > 0 && remaining > 0 ? remaining / task.speed : 0;

  useEffect(() => {
    if (!running) return;
    let alive = true;
    prev.current = { t: 0, w: [] };
    async function tick() {
      try {
        const list = await Tasks.shards(task.id);
        if (!alive) return;
        const now = Date.now();
        const dt = prev.current.t ? (now - prev.current.t) / 1000 : 0;
        setShards(
          list.map((s, i) => ({
            ...s,
            speed:
              dt > 0 &&
              prev.current.w[i] !== undefined &&
              s.written >= prev.current.w[i] &&
              s.active
                ? (s.written - prev.current.w[i]) / dt
                : 0,
          })),
        );
        setStale(false);
        prev.current = { t: now, w: list.map((s) => s.written) };
      } catch {
        // 轮询失败忽略，下个周期重试
      }
    }
    void tick();
    const timer = window.setInterval(() => void tick(), 1000);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [task.id, running]);

  // 打开时任务未在运行：拉一次快照展示冻结状态。
  useEffect(() => {
    if (running) return;
    void Tasks.shards(task.id).then((list) => {
      setShards(list.map((s) => ({ ...s, speed: 0 })));
      setStale(true);
    });
  }, [task.id, running]);

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent title="任务详情" className="dialog-wide">
        <div className="flex flex-col gap-4">
          {/* 头部 */}
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-sm font-medium" title={task.name}>
              {task.name}
            </span>
            {isHLS && (
              <Badge size="sm" color="violet">
                HLS
              </Badge>
            )}
            <Badge size="sm" tone={statusTone(task.status)}>
              {STATUS_LABEL[task.status]}
            </Badge>
          </div>

          {/* 总览 */}
          <div className="flex flex-col gap-1.5">
            {running && !isHLS && task.total < 0 ? (
              <ProgressIndeterminate />
            ) : (
              <Progress value={fraction * 100} />
            )}
            <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted">
              <span>
                {isHLS
                  ? `分片 ${task.segDone ?? 0}/${task.segTotal ?? 0}`
                  : formatProgress(task.downloaded, task.total)}
              </span>
              {fraction > 0 && <span>{(fraction * 100).toFixed(1)}%</span>}
              {task.speed > 0 && <span className="text-accent">{formatSpeed(task.speed)}</span>}
              {eta > 0 && <span>剩余约 {formatDuration(eta)}</span>}
            </div>
          </div>

          {/* 分片明细 */}
          {isHLS ? (
            <div className="rounded-lg bg-surface-muted/40 p-3 text-xs leading-5 text-muted">
              HLS 分片由并发池下载（{task.threads || "默认"} 并发），逐个分片转瞬即逝，
              这里展示整体计数：已完成 {task.segDone ?? 0} / 共 {task.segTotal ?? 0} 片。
              {stale && !running && "（以下载中断时的状态为准）"}
            </div>
          ) : shards.length > 0 ? (
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between text-[11px] text-muted">
                <span>
                  分片明细（{shards.length} 个）
                  {stale && " · 冻结于暂停前"}
                </span>
                {running && <span className="animate-pulse">实时刷新中…</span>}
              </div>
              <div className="max-h-64 overflow-y-auto rounded-lg border border-border">
                {shards.map((s) => (
                  <ShardRow key={s.index} shard={s} />
                ))}
              </div>
            </div>
          ) : (
            <div className="rounded-lg bg-surface-muted/40 p-3 text-xs text-muted">
              暂无分片信息{running && "（等待下载开始…）"}
            </div>
          )}

          {/* 元信息 */}
          <div className="flex flex-col gap-1 rounded-lg bg-surface-muted/40 p-3 text-xs text-muted">
            <MetaRow label="链接" value={task.url} />
            <MetaRow label="保存到" value={task.dir} />
            <div className="flex items-center justify-between gap-3">
              <span className="shrink-0 text-muted/70">创建于</span>
              <span className="min-w-0 truncate">
                {new Date(task.createdAt).toLocaleString()}
              </span>
            </div>
            {task.status === "done" && (task.duration ?? 0) > 0 && (
              <div className="flex items-center justify-between gap-3">
                <span className="shrink-0 text-muted/70">时长</span>
                <span className="min-w-0 truncate">{formatDuration(task.duration ?? 0)}</span>
              </div>
            )}
          </div>

          <div className="flex items-center justify-end">
            <Button variant="secondary" onClick={onClose}>
              关闭
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="shrink-0 text-muted/70">{label}</span>
      <span className="min-w-0 truncate" title={value}>
        {value}
      </span>
    </div>
  );
}

/** ShardRow：一个分片的实时状态行。 */
function ShardRow({ shard }: { shard: ShardView }) {
  const size = shard.end < 0 ? -1 : shard.end - shard.start + 1;
  const done = size >= 0 && shard.written >= size;
  const fraction = size > 0 ? Math.min(1, shard.written / size) : 0;
  const state = done
    ? { label: "完成", dot: "bg-success" }
    : shard.retrying > 0
      ? { label: `重试中 ×${shard.retrying}`, dot: "bg-warning" }
      : shard.active
        ? { label: "下载中", dot: "bg-accent animate-pulse" }
        : { label: "等待", dot: "bg-muted/50" };

  return (
    <div className="flex items-center gap-2 border-b border-border px-2.5 py-1.5 text-xs last:border-b-0">
      <span className={cn("size-1.5 shrink-0 rounded-full", state.dot)} title={state.label} />
      <span className="w-7 shrink-0 text-muted">#{shard.index}</span>
      <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-surface-muted">
        <div
          className="h-full rounded-full bg-accent transition-[width] duration-200"
          style={{ width: `${fraction * 100}%` }}
        />
      </div>
      <span className="w-28 shrink-0 text-right text-muted tabular-nums">
        {size < 0
          ? `${formatBytes(shard.written)} / 未定长`
          : `${formatBytes(shard.written)} / ${formatBytes(size)}`}
      </span>
      <span className="w-16 shrink-0 text-right text-muted tabular-nums">
        {shard.speed > 0 ? formatSpeed(shard.speed) : done ? "" : "—"}
      </span>
    </div>
  );
}
