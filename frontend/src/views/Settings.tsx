import { useEffect, useState } from "react";
import { App, Ext, type ExtStatus, type Settings as SettingsData } from "../mygo";
import { Alert } from "../components/arc/alert/alert";
import { Badge } from "../components/arc/badge/badge";
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

/** 浏览器插件行状态徽标：未检测 / 手动 / 未安装 / 待信任 / 需更新 / 已安装。 */
function extBadge(b: ExtStatus["browsers"][number]) {
  if (!b.detected) return <Badge size="sm" tone="neutral">未检测</Badge>;
  if (b.manual) return <Badge size="sm" tone="neutral" dot>手动</Badge>;
  if (b.installed && b.trusted) {
    return b.upToDate
      ? <Badge size="sm" tone="success" dot>已安装</Badge>
      : <Badge size="sm" tone="warning" dot>需更新</Badge>;
  }
  if (b.installed) return <Badge size="sm" tone="warning" dot>待信任</Badge>;
  return <Badge size="sm" tone="info" dot>未安装</Badge>;
}

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
  const [ytDlpPath, setYtDlpPath] = useState<string | null>(null);
  const [checkingYtDlp, setCheckingYtDlp] = useState(true);
  const [downloadingYtDlp, setDownloadingYtDlp] = useState(false);
  const [extError, setExtError] = useState<string | null>(null);
  const [webURL, setWebURL] = useState("");
  const [ext, setExt] = useState<ExtStatus | null>(null);
  const [extBusy, setExtBusy] = useState<string | null>(null);
  const [extHint, setExtHint] = useState<string | null>(null);

  useEffect(() => {
    App.getSettings()
      .then(setData)
      .catch((err) => setError(String(err)));
    App.checkFfmpeg()
      .then((p) => setFfmpegPath(p || null))
      .catch(() => setFfmpegPath(null))
      .finally(() => setChecking(false));
    App.checkYtDlp()
      .then((p) => setYtDlpPath(p || null))
      .catch(() => setYtDlpPath(null))
      .finally(() => setCheckingYtDlp(false));
    App.webURL()
      .then(setWebURL)
      .catch(() => setWebURL(""));
    Ext.status()
      .then(setExt)
      .catch(() => setExt(null));
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

  async function fetchYtDlp() {
    setDownloadingYtDlp(true);
    try {
      const p = await App.downloadYtDlp();
      setYtDlpPath(p);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDownloadingYtDlp(false);
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

  /** 安装/授权/移除插件到指定浏览器，完成后刷新状态。 */
  async function extAction(id: string, op: "install" | "authorize" | "uninstall") {
    setExtBusy(id);
    setExtError(null);
    setExtHint(null);
    try {
      if (op === "install") await Ext.install(id);
      else if (op === "authorize") await Ext.authorize(id);
      else await Ext.uninstall(id);
      setExt(await Ext.status());
      setExtHint(
        op === "uninstall"
          ? "已移除，重启浏览器后扩展消失"
          : "已生效，浏览器运行中通常几秒内出现（或重启浏览器）；浏览器可能显示「由您的组织管理」标记",
      );
    } catch (err) {
      setExtError(err instanceof Error ? err.message : String(err));
    } finally {
      setExtBusy(null);
    }
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

        {/* yt-dlp（YouTube 下载） */}
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <Input
              label="yt-dlp 路径"
              placeholder="手动指定 yt-dlp 路径（可选）"
              value={data.ytDlpPath}
              onChange={(e) => update({ ytDlpPath: e.target.value })}
              className="min-w-0 flex-1"
            />
            <div className="mt-5">
              <Button
                variant="secondary"
                onClick={() => void fetchYtDlp()}
                loading={downloadingYtDlp}
                disabled={downloadingYtDlp || !!ytDlpPath}
              >
                获取 yt-dlp
              </Button>
            </div>
          </div>
          <p className="text-xs leading-5 text-muted">
            {ytDlpPath
              ? `已就绪：${ytDlpPath}`
              : checkingYtDlp
                ? "检测中…"
                : "未安装。YouTube 下载需要它，点击「获取 yt-dlp」自动下载并校验（约 30 MB，仅首次）"}
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

        {/* 浏览器插件 */}
        <div className="flex flex-col gap-2">
          <div className="text-sm font-medium">浏览器插件</div>
          <p className="text-xs leading-5 text-muted">
            一键把 Knot 插件安装到本机浏览器，日常浏览时即可嗅探资源、右键或弹窗一键回传下载。
            浏览器运行中通常几秒内自动出现，否则重启浏览器后生效。
            Chrome/Edge 会把非商店来源的扩展标记为「可能已损坏」并禁用，需「管理员授权」写入本机信任策略放行
            （浏览器随后可能显示「由您的组织管理」标记）。
          </p>
          {ext && (
            <div className="flex flex-col divide-y divide-border rounded-lg border border-border">
              {ext.browsers.map((b) => (
                <div key={b.id} className="flex items-center gap-3 px-3 py-2.5">
                  <span className="w-32 shrink-0 truncate text-sm">{b.name}</span>
                  {extBadge(b)}
                  <div className="flex flex-1 items-center justify-end gap-2">
                    {!b.detected && (
                      <span className="text-xs text-muted">本机未安装该浏览器</span>
                    )}
                    {b.detected && b.manual && (
                      <span className="text-xs text-muted">无自动通道，需在扩展页手动加载</span>
                    )}
                    {b.detected && !b.manual && (
                      <>
                        {b.installed && !b.trusted && (
                          <span className="text-xs text-muted">需写入信任策略后浏览器才会启用</span>
                        )}
                        {b.installed && b.trusted && !b.upToDate && (
                          <span className="text-xs text-muted">版本待更新</span>
                        )}
                        {b.installed && (
                          <Button
                            variant="secondary"
                            size="sm"
                            loading={extBusy === b.id}
                            disabled={extBusy !== null}
                            onClick={() => void extAction(b.id, "uninstall")}
                          >
                            移除
                          </Button>
                        )}
                        {b.installed && b.trusted && b.upToDate ? (
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => void Ext.openExtensionsPage(b.id).catch((err) => setExtError(String(err)))}
                          >
                            扩展页
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            loading={extBusy === b.id}
                            disabled={extBusy !== null}
                            onClick={() => void extAction(b.id, b.installed && !b.trusted ? "authorize" : "install")}
                          >
                            {b.installed && !b.trusted ? "管理员授权" : b.installed ? "更新" : "安装"}
                          </Button>
                        )}
                      </>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {ext && (
            <p className="text-xs leading-5 text-muted">
              版本 {ext.version}（ID {ext.id}）。Firefox 需 Mozilla 签名无自动通道；
              也可在扩展管理页开启开发者模式后「加载已解压的扩展程序」选择{' '}
              <button
                type="button"
                className="text-accent underline underline-offset-2"
                onClick={() => void Ext.openDir().catch((err) => setExtError(String(err)))}
              >
                unpacked 目录
              </button>
              。
            </p>
          )}
          {extHint && !extError && (
            <Alert tone="success" title="操作成功">
              {extHint}
            </Alert>
          )}
          {extError && (
            <Alert tone="danger" title="插件操作失败">
              {extError}
            </Alert>
          )}
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
