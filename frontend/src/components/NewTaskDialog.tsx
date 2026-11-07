import { useState } from "react";
import { App, Tasks, type Preview } from "../mygo";
import { formatBytes } from "../lib/format";
import { Button, Input, Modal } from "./ui";
import { IconDownload } from "./icons";

/**
 * 新建下载：粘贴 URL → 解析预览 → 创建任务。
 * 解析失败时仍可强制添加（例如个别服务器对探测请求不友好）。
 */
export function NewTaskDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  // 仅在打开时挂载表单，关闭即销毁，state 每次都是全新的。
  if (!open) return null;
  return (
    <Modal open onClose={onClose} title="新建下载">
      <NewTaskForm onClose={onClose} />
    </Modal>
  );
}

function NewTaskForm({ onClose }: { onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [parsing, setParsing] = useState(false);
  const [creating, setCreating] = useState(false);

  async function parse() {
    const trimmed = url.trim();
    if (!trimmed) return;
    setParsing(true);
    setError(null);
    setPreview(null);
    try {
      const p = await App.parseURL(trimmed);
      setPreview(p);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setParsing(false);
    }
  }

  async function create() {
    setCreating(true);
    try {
      await Tasks.create(url.trim(), {});
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <label className="text-xs text-muted" htmlFor="new-task-url">
          下载链接（http/https 直链）
        </label>
        <div className="flex gap-2">
          <Input
            id="new-task-url"
            autoFocus
            placeholder="https://example.com/file.zip"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && url.trim() && !preview) void parse();
            }}
            className="min-w-0 flex-1"
          />
          <Button onClick={() => void parse()} disabled={!url.trim() || parsing}>
            {parsing ? "解析中…" : "解析"}
          </Button>
        </div>
      </div>

        {preview && (
          <div className="rounded-lg border border-border bg-panel-2 p-3 text-sm">
            <div className="truncate font-medium" title={preview.name}>
              {preview.name}
            </div>
            <div className="mt-1 text-xs text-muted">
              {preview.kind === "hls" ? (
                <>
                  <span className="font-medium text-violet-500">HLS 视频流</span>
                  {" · 下载完成后自动合并为 MP4"}
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

      {error && (
        <div className="rounded-lg border border-danger/30 bg-danger/10 p-3 text-xs text-danger">
          {error}
        </div>
      )}

      <div className="flex items-center justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>
          取消
        </Button>
        {preview ? (
          <Button onClick={() => void create()} disabled={creating}>
            <IconDownload className="size-4" />
            {creating ? "添加中…" : "开始下载"}
          </Button>
        ) : (
          error && (
            <Button onClick={() => void create()} disabled={creating}>
              仍然添加
            </Button>
          )
        )}
      </div>
    </div>
  );
}
