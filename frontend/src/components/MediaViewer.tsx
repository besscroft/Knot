import { useEffect, useRef, useState } from "react";
import Player from "xgplayer";
import "xgplayer/dist/index.min.css";
import { ExternalLink, X } from "lucide-react";
import { Tasks, type Task } from "../mygo";
import { Button } from "./arc/button/button";
import { IconButton } from "./ui";
import { mediaFileUrl, mediaKindOf } from "../lib/format";

/**
 * MediaViewer：应用内查看/播放已完成任务——视频用 xgplayer（自带进度、
 * 倍速、画中画、网页全屏与快捷键），图片用 <img>。加载/解码失败时给出
 * 系统播放器兜底。仅在非浏览状态下使用（浏览时站点子窗口会盖住主窗口内容区）。
 */
export function MediaViewer({ task, onClose }: { task: Task; onClose: () => void }) {
  const [failed, setFailed] = useState(false);
  const boxRef = useRef<HTMLDivElement | null>(null);
  const kind = mediaKindOf(task.name);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    if (kind !== "video" || !boxRef.current) return;
    const player = new Player({
      el: boxRef.current,
      url: mediaFileUrl(task.id),
      lang: "zh-cn",
      autoplay: true,
      width: "100%",
      height: "100%",
      fitVideoSize: "fixed", // 播放器占满容器，视频按比例加黑边
      videoFillMode: "contain",
      pip: true,
      cssFullscreen: true,
      playbackRate: [0.5, 0.75, 1, 1.25, 1.5, 2],
      keyShortcut: true,
      enableContextmenu: false,
    });
    player.on("error", () => setFailed(true));
    return () => {
      player.destroy();
    };
  }, [kind, task.id]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="dialog"
      aria-modal="true"
      aria-label={task.name}
    >
      {/* 顶部栏：文件名 + 系统播放器 + 关闭 */}
      <div className="absolute left-0 right-0 top-0 flex h-12 items-center justify-between gap-4 px-4">
        <span className="min-w-0 truncate text-sm text-white/90" title={task.name}>
          {task.name}
        </span>
        <div className="flex shrink-0 items-center gap-1">
          <IconButton
            title="用系统播放器打开"
            className="text-white/80 hover:bg-white/15 hover:text-white"
            onClick={() => void Tasks.openPath(task.id)}
          >
            <ExternalLink className="size-4" />
          </IconButton>
          <IconButton
            title="关闭（Esc）"
            className="text-white/80 hover:bg-white/15 hover:text-white"
            onClick={onClose}
          >
            <X className="size-5" />
          </IconButton>
        </div>
      </div>

      {kind === "image" ? (
        <img
          src={mediaFileUrl(task.id)}
          className="max-h-[82vh] max-w-full rounded-lg object-contain"
          alt={task.name}
        />
      ) : failed ? (
        <div className="flex flex-col items-center gap-3">
          <div className="text-sm text-white/80">无法在应用内播放该格式，可尝试系统播放器</div>
          <Button variant="secondary" onClick={() => void Tasks.openPath(task.id)}>
            <ExternalLink className="size-4" />
            用系统播放器打开
          </Button>
        </div>
      ) : (
        // 外层管布局尺寸；内层是 xgplayer 的挂载点——它会把 width/height
        // 以内联样式写到挂载元素上（100%），必须与布局层分开才不会被撑爆。
        <div className="flex h-[80vh] w-[90vw] max-w-[1400px] overflow-hidden rounded-lg bg-black">
          <div ref={boxRef} className="h-full w-full" />
        </div>
      )}
    </div>
  );
}
