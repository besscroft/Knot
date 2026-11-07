import { useEffect, useState } from "react";
import { App, type Settings as SettingsData } from "../mygo";
import { Button, Input } from "../components/ui";
import { setMediaBase } from "../lib/format";

/** 嗅探资源类型枚举，与后端 AllSniffTypes / 嗅探面板的 type 值一致。 */
const SNIFF_TYPES: { value: string; label: string }[] = [
  { value: "hls", label: "HLS" },
  { value: "video", label: "视频" },
  { value: "audio", label: "音频" },
  { value: "image", label: "图片" },
  { value: "archive", label: "压缩包" },
  { value: "document", label: "文档" },
  { value: "link", label: "链接" },
  { value: "other", label: "其他" },
];

/** 设置页：下载目录、分片数、并发数、格式、FFmpeg 与 Web 文件库。 */
export function SettingsView() {
  const [data, setData] = useState<SettingsData | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [ffmpegPath, setFfmpegPath] = useState<string | null>(null);
  const [checking, setChecking] = useState(true);
  const [downloadingFfmpeg, setDownloadingFfmpeg] = useState(false);
  const [webURL, setWebURL] = useState("");

  useEffect(() => {
    App.getSettings()
      .then(setData)
      .catch((err) => setError(String(err)));
    App.checkFfmpeg()
      .then((p) => setFfmpegPath(p || null))
      .catch(() => setFfmpegPath(null))
      .finally(() => setChecking(false));
    App.webURL()
      .then(setWebURL)
      .catch(() => setWebURL(""));
  }, []);

  // 拉取最新媒体服务地址（监听方式/端口变化重启后调用）。
  function refreshMedia() {
    App.mediaBase()
      .then(setMediaBase)
      .catch(() => {});
    App.webURL()
      .then(setWebURL)
      .catch(() => setWebURL(""));
  }

  async function fetchFfmpeg() {
    setDownloadingFfmpeg(true);
    try {
      const p = await App.downloadFfmpeg();
      setFfmpegPath(p);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDownloadingFfmpeg(false);
    }
  }

  useEffect(() => {
    App.getSettings()
      .then(setData)
      .catch((err) => setError(String(err)));
  }, []);

  function update(patch: Partial<SettingsData>) {
    setData((d) => (d ? { ...d, ...patch } : d));
    setDirty(true);
    setSaved(false);
  }

  async function pickFolder() {
    const dir = await App.pickFolder();
    if (dir) update({ downloadDir: dir });
  }

  async function save() {
    if (!data) return;
    if (!data.sniffTypes || data.sniffTypes.length === 0) {
      setError("嗅探资源类型至少勾选一项");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await App.saveSettings(data);
      setDirty(false);
      setSaved(true);
      refreshMedia(); // Web 监听方式/端口可能已变化，刷新媒体与文件库地址
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  if (!data) {
    return <div className="p-6 text-sm text-muted">{error ?? "加载中…"}</div>;
  }

  return (
    <div className="mx-auto max-w-xl p-6">
      <h1 className="mb-6 text-lg font-semibold">设置</h1>

      <div className="flex flex-col gap-5">
        <Row label="下载目录" hint="新任务的默认保存位置">
          <div className="flex gap-2">
            <Input value={data.downloadDir} readOnly className="flex-1" />
            <Button variant="secondary" onClick={() => void pickFolder()}>
              选择…
            </Button>
          </div>
        </Row>

        <Row label="单任务分片数" hint="每个任务并行下载的连接数（1-64）">
          <Input
            type="number"
            min={1}
            max={64}
            value={data.threads}
            onChange={(e) => update({ threads: Number(e.target.value) })}
            className="w-28"
          />
        </Row>

        <Row label="同时下载任务数" hint="队列并发上限（1-10）">
          <Input
            type="number"
            min={1}
            max={10}
            value={data.maxConcurrent}
            onChange={(e) => update({ maxConcurrent: Number(e.target.value) })}
            className="w-28"
          />
        </Row>

        <Row label="下载代理" hint="作用于下载器；留空跟随系统/环境代理，支持 http://、socks5://，内置浏览始终走系统代理">
          <Input
            placeholder="如 http://127.0.0.1:7890"
            value={data.proxy}
            onChange={(e) => update({ proxy: e.target.value })}
            className="w-72"
          />
        </Row>

        <Row label="视频默认格式" hint="新建视频任务未选择时的默认容器">
          <select
            value={data.videoFormat}
            onChange={(e) => update({ videoFormat: e.target.value })}
            className="h-8 rounded-md border border-border bg-panel px-2 text-sm text-text focus:border-accent focus:outline-none"
          >
            <option value="">MP4（默认）</option>
            <option value="mkv">MKV</option>
            <option value="mov">MOV</option>
            <option value="webm">WebM（需源编码兼容）</option>
          </select>
        </Row>

        <Row label="音频默认格式" hint="纯音频链接未选择时的默认格式">
          <select
            value={data.audioFormat}
            onChange={(e) => update({ audioFormat: e.target.value })}
            className="h-8 rounded-md border border-border bg-panel px-2 text-sm text-text focus:border-accent focus:outline-none"
          >
            <option value="">不转换（原样保存）</option>
            <option value="m4a">M4A</option>
            <option value="mp3">MP3</option>
            <option value="opus">Opus</option>
            <option value="flac">FLAC</option>
            <option value="wav">WAV</option>
          </select>
        </Row>

        <Row
          label="FFmpeg"
          hint={
            ffmpegPath
              ? `已就绪：${ffmpegPath}`
              : checking
                ? "检测中…"
                : "未安装。格式转换需要它，点击「获取 FFmpeg」自动下载并校验（约 80 MB，仅首次）"
          }
        >
          <div className="flex items-center gap-2">
            <Input
              placeholder="手动指定 ffmpeg 路径（可选）"
              value={data.ffmpegPath}
              onChange={(e) => update({ ffmpegPath: e.target.value })}
              className="w-56"
            />
            <Button
              variant="secondary"
              onClick={() => void fetchFfmpeg()}
              disabled={downloadingFfmpeg || !!ffmpegPath}
            >
              {downloadingFfmpeg ? "下载中…" : "获取 FFmpeg"}
            </Button>
          </div>
        </Row>

        <Row
          label="Web 文件库"
          hint={
            data.webAccess
              ? webURL
                ? `手机等设备在浏览器访问 ${webURL} 即可查看、播放与下载已完成的文件；连接为明文 HTTP，请仅在可信网络开启，首次可能需允许系统防火墙`
                : "开启后同一网络内的设备（如手机）在浏览器输入本机 IP 与端口即可直接访问"
              : `关闭时仅本机可访问 http://127.0.0.1:${data.webPort}/，应用内播放不受影响`
          }
        >
          <div className="flex items-center gap-3">
            <label className="flex cursor-pointer items-center gap-1.5 text-xs">
              <input
                type="checkbox"
                className="size-3.5 accent-[var(--accent)]"
                checked={data.webAccess}
                onChange={(e) => update({ webAccess: e.target.checked })}
              />
              局域网访问
            </label>
            <Input
              type="number"
              min={1024}
              max={65535}
              value={data.webPort}
              onChange={(e) => update({ webPort: Number(e.target.value) })}
              className="w-24"
            />
          </div>
        </Row>

        <Row label="嗅探资源类型" hint="内置浏览器只收集勾选的类型，保存后即时生效（至少一项）">
          <div className="flex w-64 flex-wrap gap-x-3 gap-y-1.5">
            {SNIFF_TYPES.map((t) => (
              <label key={t.value} className="flex cursor-pointer items-center gap-1.5 text-xs">
                <input
                  type="checkbox"
                  className="size-3.5 accent-[var(--accent)]"
                  checked={data.sniffTypes.includes(t.value)}
                  onChange={(e) => {
                    const next = e.target.checked
                      ? [...data.sniffTypes, t.value]
                      : data.sniffTypes.filter((v) => v !== t.value);
                    update({ sniffTypes: next });
                  }}
                />
                {t.label}
              </label>
            ))}
          </div>
        </Row>

        {error && <div className="text-xs text-danger">{error}</div>}

        <div className="flex items-center gap-3">
          <Button onClick={() => void save()} disabled={!dirty || saving}>
            {saving ? "保存中…" : "保存"}
          </Button>
          {saved && !dirty && <span className="text-xs text-success">已保存</span>}
        </div>
      </div>
    </div>
  );
}

function Row({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-6">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {hint && <div className="mt-0.5 text-xs text-muted">{hint}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}
