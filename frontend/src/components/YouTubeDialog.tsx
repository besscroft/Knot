import { useEffect, useState } from "react";
import { Download } from "lucide-react";
import { App, Tasks, type Sniffed, type YouTubeInfo } from "../mygo";
import { Alert } from "./arc/alert/alert";
import { Button } from "./arc/button/button";
import { Dialog, DialogContent } from "./arc/dialog/dialog";
import { Input } from "./arc/input/input";
import { Select } from "./arc/select/select";
import { formatBytes, formatDuration } from "../lib/format";

/** YouTube 保存格式选项（视频/音频用前缀分组，Arc Select 不支持 optgroup）。 */
const YT_FORMAT_OPTIONS = [
  { value: "mp4", label: "视频 · MP4（无需其他工具即可播放）" },
  { value: "mkv", label: "视频 · MKV" },
  { value: "mov", label: "视频 · MOV" },
  { value: "webm", label: "视频 · WebM（源编码兼容时免转码）" },
  { value: "m4a", label: "仅音频 · M4A" },
  { value: "mp3", label: "仅音频 · MP3" },
  { value: "opus", label: "仅音频 · Opus" },
  { value: "flac", label: "仅音频 · FLAC" },
  { value: "wav", label: "仅音频 · WAV" },
];

/**
 * YouTubeForm：画质/格式/文件名选择表单，粘贴链接（新建下载弹窗）与
 * 嗅探浮窗（YouTubeDialog）共用。挂载即通过 yt-dlp 解析，解析与下载
 * 都在后端完成（首次使用会自动获取 yt-dlp）。
 */
export function YouTubeForm({ url, onClose }: { url: string; onClose: () => void }) {
  const [info, setInfo] = useState<YouTubeInfo | null>(null);
  const [error, setError] = useState("");
  const [quality, setQuality] = useState(0);
  const [format, setFormat] = useState("mp4");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    App.resolveYouTube(url)
      .then((v) => {
        if (!alive) return;
        setInfo(v);
        if (v.qualities.length > 0) setQuality(v.qualities[0].id); // 默认最高画质
        setName(v.title);
      })
      .catch((e: unknown) => {
        if (alive) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      alive = false;
    };
  }, [url]);

  async function submit() {
    setBusy(true);
    setError("");
    try {
      await Tasks.createYouTube(url, quality, { format, name: name.trim() });
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="min-w-0">
        <div className="truncate text-sm font-medium" title={info?.title ?? url}>
          {info?.title ?? url}
        </div>
        {info && (
          <div className="mt-0.5 text-[11px] text-muted">
            yt-dlp 解析 · 视频+音频自动合流
            {(info.duration ?? 0) > 0 && ` · 时长 ${formatDuration(info.duration)}`}
          </div>
        )}
      </div>
      {!info && !error && (
        <div className="text-xs text-muted">正在读取画质信息…（首次使用会先自动获取 yt-dlp）</div>
      )}
      {error && (
        <Alert tone="danger" title="解析失败">
          {error}
        </Alert>
      )}
      {info && (
        <>
          <Select
            label="画质"
            value={String(quality)}
            onValueChange={(v) => setQuality(Number(v))}
            options={info.qualities.map((q) => ({
              value: String(q.id),
              label: `${q.desc}${q.size > 0 ? ` · 约 ${formatBytes(q.size)}` : ""}${
                q.codecs ? ` · ${q.codecs}` : ""
              }`,
            }))}
          />
          <Select label="格式" value={format} onValueChange={setFormat} options={YT_FORMAT_OPTIONS} />
          <Input
            label="文件名"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onFocus={(e) => e.target.select()}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !busy) void submit();
            }}
          />
        </>
      )}
      <div className="flex items-center justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>
          取消
        </Button>
        <Button onClick={() => void submit()} loading={busy} disabled={!info || busy}>
          <Download className="size-4" />
          下载
        </Button>
      </div>
    </div>
  );
}

/** YouTubeDialog：嗅探浮窗的 YouTube 视频下载弹窗。 */
export function YouTubeDialog({
  entry,
  onQueued,
  onClose,
}: {
  entry: Sniffed;
  onQueued: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent title="YouTube 视频下载">
        <YouTubeForm
          url={entry.url}
          onClose={() => {
            onQueued();
            onClose();
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
