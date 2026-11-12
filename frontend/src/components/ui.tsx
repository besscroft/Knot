import type { ButtonHTMLAttributes } from "react";
import { cn } from "../lib/utils";

/**
 * 基于 Arc UI 的本地薄适配层：只放 Arc 组件库里没有、但 Knot 需要的小件。
 * Button / Input / Select / Checkbox / Switch / Progress / Dialog / Badge /
 * Card / Alert / EmptyState / SegmentedControl 等一律直接用 components/arc/ 下的
 * Arc 组件，不再在这里包一层。
 */

/** IconButton：无边框的小型图标按钮。Arc 未提供紧凑图标按钮，用 Arc 令牌实现同观感。 */
export function IconButton({
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex size-7 shrink-0 items-center justify-center rounded-full text-muted transition-colors",
        "hover:bg-surface-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40",
        className,
      )}
      {...props}
    />
  );
}

/** ProgressIndeterminate：未知大小的流动进度条（Arc Progress 不含 indeterminate）。 */
export function ProgressIndeterminate({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "h-[7px] w-full overflow-hidden rounded-full border border-border bg-surface-muted",
        className,
      )}
    >
      <div className="progress-indeterminate h-full w-1/3 rounded-full bg-accent" />
    </div>
  );
}
