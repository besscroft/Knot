import { useEffect, useState } from "react";
import { App, type Settings as SettingsData } from "../mygo";
import { Alert } from "../components/arc/alert/alert";
import { Button } from "../components/arc/button/button";
import { Checkbox } from "../components/arc/checkbox/checkbox";
import { Input } from "../components/arc/input/input";
import { Select } from "../components/arc/select/select";
import { Switch } from "../components/arc/switch/switch";
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

const VIDEO_FORMAT_DEFAULT = "__default"; // 「跟随设置」在 Select 里的占位值
const AUDIO_FORMAT_DEFAULT = "__default";

/** 设置页：下载目录、分片数、并发数、格式、FFmpeg 与 Web 文件库。
 *  字段用 Arc 的纵向布局（label + description 由控件自带），分组纵向排列。 */
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
    <div className="h-full overflow-y-auto p-6">
      <h1 className="mb-6 text-lg font-semibold">设置</h1>

      <div className="flex flex-col gap-6">
        {/* 下载目录 */}
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <Input
              label="下载目录"
              value={data.downloadDir}
              readOnly
              className="min-w-0 flex-1"
            />
            <div className="mt-5">
              <Button variant="secondary" onClick={() => void pickFolder()}>
                选择…
              </Button>
            </div>
          </div>
          <p className="text-xs text-muted">新任务的默认保存位置</p>
        </div>

        <div className="flex gap-6">
          <div className="w-32">
            <Input
              label="单任务分片数"
              type="number"
              min={1}
              max={64}
              value={data.threads}
              onChange={(e) => update({ threads: Number(e.target.value) })}
            />
          </div>
          <div className="w-32">
            <Input
              label="同时下载任务数"
              type="number"
              min={1}
              max={10}
              value={data.maxConcurrent}
              onChange={(e) => update({ maxConcurrent: Number(e.target.value) })}
            />
          </div>
        </div>
        <p className="-mt-4 text-xs text-muted">
          分片数：每个任务并行下载的连接数（1-64）；任务数：队列并发上限（1-10）
        </p>

        <Input
          label="下载代理"
          description="留空跟随系统/环境代理，支持 http://、socks5://，内置浏览始终走系统代理"
          placeholder="如 http://127.0.0.1:7890"
          value={data.proxy}
          onChange={(e) => update({ proxy: e.target.value })}
        />

        {/* 默认格式：两个下拉并排 */}
        <div className="grid grid-cols-2 gap-6">
          <Select
            label="视频默认格式"
            description="新建视频任务未选择时的默认容器"
            value={data.videoFormat || VIDEO_FORMAT_DEFAULT}
            onValueChange={(v) => update({ videoFormat: v === VIDEO_FORMAT_DEFAULT ? "" : v })}
            options={[
              { value: VIDEO_FORMAT_DEFAULT, label: "MP4（默认）" },
              { value: "mkv", label: "MKV" },
              { value: "mov", label: "MOV" },
              { value: "webm", label: "WebM（需源编码兼容）" },
            ]}
          />

          <Select
            label="音频默认格式"
            description="纯音频链接未选择时的默认格式"
            value={data.audioFormat || AUDIO_FORMAT_DEFAULT}
            onValueChange={(v) => update({ audioFormat: v === AUDIO_FORMAT_DEFAULT ? "" : v })}
            options={[
              { value: AUDIO_FORMAT_DEFAULT, label: "不转换（原样保存）" },
              { value: "m4a", label: "M4A" },
              { value: "mp3", label: "MP3" },
              { value: "opus", label: "Opus" },
              { value: "flac", label: "FLAC" },
              { value: "wav", label: "WAV" },
            ]}
          />
        </div>

        {/* FFmpeg */}
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <Input
              label="FFmpeg 路径"
              placeholder="手动指定 ffmpeg 路径（可选）"
              value={data.ffmpegPath}
              onChange={(e) => update({ ffmpegPath: e.target.value })}
              className="min-w-0 flex-1"
            />
            <div className="mt-5">
              <Button
                variant="secondary"
                onClick={() => void fetchFfmpeg()}
                loading={downloadingFfmpeg}
                disabled={downloadingFfmpeg || !!ffmpegPath}
              >
                获取 FFmpeg
              </Button>
            </div>
          </div>
          <p className="text-xs leading-5 text-muted">
            {ffmpegPath
              ? `已就绪：${ffmpegPath}`
              : checking
                ? "检测中…"
                : "未安装。格式转换需要它，点击「获取 FFmpeg」自动下载并校验（约 80 MB，仅首次）"}
          </p>
        </div>

        {/* Web 文件库 */}
        <div className="flex flex-col gap-2">
          <Switch
            label="局域网访问（Web 文件库）"
            checked={data.webAccess}
            onCheckedChange={(v) => update({ webAccess: v })}
          />
          <div className="w-32">
            <Input
              label="端口"
              type="number"
              min={1024}
              max={65535}
              value={data.webPort}
              onChange={(e) => update({ webPort: Number(e.target.value) })}
            />
          </div>
          <p className="text-xs leading-5 text-muted">
            {data.webAccess
              ? webURL
                ? `手机等设备在浏览器访问 ${webURL} 即可查看、播放与下载已完成的文件；连接为明文 HTTP，请仅在可信网络开启，首次可能需允许系统防火墙`
                : "开启后同一网络内的设备（如手机）在浏览器输入本机 IP 与端口即可直接访问"
              : `关闭时仅本机可访问 http://127.0.0.1:${data.webPort}/，应用内播放不受影响`}
          </p>
        </div>

        {/* 嗅探资源类型 */}
        <div className="flex flex-col gap-2">
          <div className="text-sm font-medium">嗅探资源类型</div>
          <p className="text-xs text-muted">
            内置浏览器只收集勾选的类型，保存后即时生效（至少一项）
          </p>
          <div className="flex flex-wrap gap-x-5 gap-y-2">
            {SNIFF_TYPES.map((t) => (
              <Checkbox
                key={t.value}
                label={t.label}
                checked={data.sniffTypes.includes(t.value)}
                onCheckedChange={(v) => {
                  const next = v
                    ? [...data.sniffTypes, t.value]
                    : data.sniffTypes.filter((x) => x !== t.value);
                  update({ sniffTypes: next });
                }}
              />
            ))}
          </div>
        </div>

        {error && (
          <Alert tone="danger" title="保存失败">
            {error}
          </Alert>
        )}
        {saved && !dirty && (
          <Alert tone="success" title="已保存">
            设置已生效
          </Alert>
        )}

        <div className="flex items-center gap-3">
          <Button onClick={() => void save()} loading={saving} disabled={!dirty || saving}>
            保存
          </Button>
        </div>
      </div>
    </div>
  );
}
