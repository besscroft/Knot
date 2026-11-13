import { useState } from "react";
import { Download } from "lucide-react";
import { App, Tasks, type Preview } from "../mygo";
import { formatBytes } from "../lib/format";
import { Alert } from "./arc/alert/alert";
import { Button } from "./arc/button/button";
import { Dialog, DialogContent } from "./arc/dialog/dialog";
import { Input } from "./arc/input/input";
import { Select } from "./arc/select/select";
import { YouTubeForm } from "./YouTubeDialog";

/** 保存格式选项（视频/音频用前缀分组，Arc Select 不支持 optgroup）。 */
const FORMAT_OPTIONS = [
  { value: "mp4", label: "视频 · MP4（无需其他工具即可播放）" },
  { value: "mkv", label: "视频 · MKV" },
  { value: "mov", label: "视频 · MOV" },
  { value: "webm", label: "视频 · WebM（需源编码兼容）" },
  { value: "m4a", label: "音频 · M4A" },
  { value: "mp3", label: "音频 · MP3" },
  { value: "opus", label: "音频 · Opus" },
  { value: "flac", label: "音频 · FLAC" },
  { value: "wav", label: "音频 · WAV" },
];
const FORMAT_DEFAULT = "__default"; // 「跟随设置」在 Select 里的占位值

/**
 * 新建下载：粘贴 URL → 解析预览 → 创建任务。
 * 解析失败时仍可强制添加（例如个别服务器对探测请求不友好）。
 */
export function NewTaskDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent title="新建下载">
        {/* 仅在打开时挂载表单，关闭即销毁，state 每次都是全新的 */}
        {open ? <NewTaskForm onClose={onClose} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function NewTaskForm({ onClose }: { onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [parsing, setParsing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [format, setFormat] = useState("");

  async function parse() {
    const trimmed = url.trim();
    if (!trimmed) return;
    setParsing(true);
    setError(null);
    setPreview(null);
    try {
      const p = await App.parseURL(trimmed);
      setPreview(p);
      setName(p.name); // 预填建议文件名，可改
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setParsing(false);
    }
  }

  async function create() {
    setCreating(true);
    try {
      await Tasks.create(url.trim(), { format, name: name.trim() });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

  // YouTube 链接：画质/格式交给专用表单（解析与下载走 yt-dlp），
  // 保留链接输入框便于换个链接重新解析。
  if (preview?.kind === "youtube") {
    return (
      <div className="flex flex-col gap-3">
        <Input
          id="new-task-url"
          label="下载链接"
          autoFocus
          placeholder="粘贴链接，回车解析"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && url.trim()) void parse();
          }}
        />
        <YouTubeForm url={preview.url || url.trim()} onClose={onClose} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <Input
        id="new-task-url"
        label="下载链接（http/https 直链）"
        autoFocus
        placeholder="粘贴链接，回车解析（http/https 直链）"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && url.trim()) void parse();
        }}
      />

      {/* 解析是初始态的主行动：全宽独占一行；解析出结果后让位给预览
          （编辑链接后回车仍会重新解析） */}
      {!preview && (
        <Button onClick={() => void parse()} loading={parsing} disabled={!url.trim() || parsing}>
          解析
        </Button>
      )}

      {preview && (
        <div className="rounded-lg bg-surface-muted p-3 text-sm">
          <Input
            id="new-task-name"
            label="文件名"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onFocus={(e) => e.target.select()}
            placeholder="留空则从链接自动推断"
          />
          <div className="mt-2 text-xs text-muted">
            {preview.kind === "hls" ? (
              <>
                <span className="font-medium text-[var(--tone-violet)]">HLS 视频流</span>
                {format ? ` · 合并为 ${format.toUpperCase()}` : " · 自动合并"}
              </>
            ) : (
              <>
                大小：{formatBytes(preview.size)}
                {preview.acceptRanges ? " · 支持多线程分片" : " · 单线程下载"}
              </>
            )}
          </div>
          <div className="mt-1 truncate text-xs text-muted/70" title={preview.url}>
            {preview.url}
          </div>
        </div>
      )}

      {preview && preview.mediaType !== "other" && (
        <Select
          label="保存为"
          value={format || FORMAT_DEFAULT}
          onValueChange={(v) => setFormat(v === FORMAT_DEFAULT ? "" : v)}
          options={[
            { value: FORMAT_DEFAULT, label: "默认（跟随设置）" },
            ...FORMAT_OPTIONS,
          ]}
          description={
            format && format !== "mp4" && format !== "m4a"
              ? "转换由 FFmpeg 完成：缺失时会自动下载（约 80 MB，仅首次）"
              : "容器转换/音频提取均为流拷贝，速度很快"
          }
        />
      )}

      {error && (
        <Alert tone="danger" title="操作失败">
          {error}
        </Alert>
      )}

      {/* 底部操作只在有实际动作时出现：初始态用 × / Esc 关闭即可 */}
      {(preview || error) && (
        <div className="flex items-center justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          {preview ? (
            <Button onClick={() => void create()} loading={creating}>
              <Download className="size-4" />
              开始下载
            </Button>
          ) : (
            <Button onClick={() => void create()} loading={creating}>
              仍然添加
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
