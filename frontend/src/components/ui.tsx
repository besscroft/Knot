import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/utils";

/** Button：primary / secondary / ghost / danger 四种风格的基础按钮。 */
export function Button({
  variant = "primary",
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
}) {
  return (
    <button
      className={cn(
        "inline-flex h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md px-3 text-sm font-medium transition-colors",
        "focus-visible:outline-2 focus-visible:outline-accent disabled:pointer-events-none disabled:opacity-50",
        variant === "primary" && "bg-accent text-white hover:opacity-90",
        variant === "secondary" &&
          "border border-border bg-panel text-text hover:bg-panel-2",
        variant === "ghost" && "text-text hover:bg-panel-2",
        variant === "danger" && "text-danger hover:bg-danger/10",
        className,
      )}
      {...props}
    />
  );
}

/** IconButton：无边框的图标按钮。 */
export function IconButton({
  className,
  title,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      title={title}
      className={cn(
        "inline-flex size-7 items-center justify-center rounded-md text-muted transition-colors",
        "hover:bg-panel-2 hover:text-text disabled:pointer-events-none disabled:opacity-40",
        className,
      )}
      {...props}
    />
  );
}

/** Input：文本输入框。 */
export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "h-8 w-full rounded-md border border-border bg-panel px-2.5 text-sm text-text",
        "placeholder:text-muted/70 focus:border-accent focus:outline-none",
        className,
      )}
      {...props}
    />
  );
}

/** Progress：细进度条。value 在 0~1；indeterminate 为未知大小的流动条。 */
export function Progress({
  value,
  indeterminate = false,
  className,
}: {
  value: number;
  indeterminate?: boolean;
  className?: string;
}) {
  const pct = Math.min(100, Math.max(0, value * 100));
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-panel-2", className)}>
      {indeterminate ? (
        <div className="progress-indeterminate h-full w-1/3 rounded-full bg-accent" />
      ) : (
        <div
          className="h-full rounded-full bg-accent transition-[width] duration-200"
          style={{ width: `${pct}%` }}
        />
      )}
    </div>
  );
}

/** Modal：轻量模态层，点击遮罩或 Esc 关闭。 */
export function Modal({
  open,
  onClose,
  title,
  children,
  width = 420,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  width?: number;
}) {
  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
    >
      <div
        className="rounded-xl border border-border bg-panel p-5 shadow-xl"
        style={{ width }}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <h2 className="mb-4 text-base font-semibold">{title}</h2>
        {children}
      </div>
    </div>
  );
}

/** EmptyState：列表为空时的占位。 */
export function EmptyState({
  icon,
  text,
  hint,
}: {
  icon: ReactNode;
  text: string;
  hint?: string;
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-muted">
      <div className="text-muted/60">{icon}</div>
      <div className="text-sm">{text}</div>
      {hint ? <div className="text-xs text-muted/70">{hint}</div> : null}
    </div>
  );
}
