import { useEffect, useState } from 'react';
import { Download, Loader2, MonitorPlay, RefreshCw } from 'lucide-react';
import { fmtDuration, fmtSize } from '@/lib/format';
import type { TabData } from '@/lib/types';
import { isYouTubeVideoPage } from '@/lib/youtube';

// YouTube 卡片：与应用内 YouTubeDialog 同一管线 —— 链接交给 Knot 的
// yt-dlp 解析与下载。播放媒体走 SABR/UMP（嗅探层看不见），这张卡片是
// 插件里唯一的 YouTube 入口：打开弹窗即按需解析画质（首次使用会先自动
// 获取 yt-dlp），SPA 切视频后地址变化自动重新解析。
const FORMATS = [
  { value: 'mp4', label: 'MP4 视频' },
  { value: 'mkv', label: 'MKV 视频' },
  { value: 'webm', label: 'WebM 视频' },
  { value: 'm4a', label: 'M4A 仅音频' },
  { value: 'mp3', label: 'MP3 仅音频' },
];

interface Props {
  data: TabData;
  tabId: number;
  /** 当前活动标签页地址（SPA 导航不落 TabData，以实时地址为准）。 */
  tabUrl: string;
}

export default function YouTubeCard({ data, tabId, tabUrl }: Props) {
  const isYtPage = isYouTubeVideoPage(tabUrl);
  const state = data.youtube?.url === tabUrl ? data.youtube : null;
  const [resolving, setResolving] = useState(false);
  const [qualityId, setQualityId] = useState<number | null>(null);
  const [format, setFormat] = useState('mp4');
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);

  function resolve(force = false): void {
    if (resolving) return;
    setResolving(true);
    void browser.runtime
      .sendMessage({ type: 'resolveYouTube', tabId, url: tabUrl, force })
      .catch(() => {})
      .finally(() => setResolving(false));
  }

  // 当前地址没有结果时自动解析；失败结果落盘后不再重入，交给用户重试。
  useEffect(() => {
    if (isYtPage && !state) resolve();
    // state 变化（解析完成/失败落盘）不重入，否则失败会立刻循环重试。
  }, [isYtPage, tabUrl]);

  if (!isYtPage) return null;

  async function send(): Promise<void> {
    setSending(true);
    setResult(null);
    try {
      const res = (await browser.runtime.sendMessage({
        type: 'sendYouTube',
        url: tabUrl,
        qualityId: activeQuality,
        format,
      })) as { ok: boolean; error?: string };
      setResult({
        ok: res?.ok ?? false,
        message: res?.ok ? '已发送到 Knot' : res?.error ?? '发送失败',
      });
    } catch {
      setResult({ ok: false, message: '发送失败，请重试' });
    } finally {
      setSending(false);
    }
  }

  // 等待解析：解析期间或结果尚未落到标签页状态。
  if (resolving || !state) {
    return (
      <div className="mx-3 mt-3 flex items-center gap-2 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-700 dark:border-sky-900 dark:bg-sky-950/40 dark:text-sky-400">
        <MonitorPlay className="size-4 shrink-0" />
        <span>已识别 YouTube 视频页，正在解析画质信息…（首次使用会先自动获取 yt-dlp）</span>
      </div>
    );
  }

  // 解析失败：展示原因并允许手动重试。
  if (!state.ok) {
    return (
      <div className="mx-3 mt-3 rounded-lg border border-red-300/60 bg-red-50 px-3 py-2 text-xs text-red-600 dark:border-red-900 dark:bg-red-950/50 dark:text-red-400">
        <p className="break-words">YouTube 解析失败：{state.error || '未知原因'}</p>
        <button
          type="button"
          onClick={() => resolve(true)}
          className="mt-1.5 inline-flex items-center gap-1 font-medium underline underline-offset-2"
        >
          <RefreshCw className="size-3" />
          重试
        </button>
      </div>
    );
  }

  const audioOnly = format === 'm4a' || format === 'mp3';
  const activeQuality = qualityId ?? state.qualities[0]?.id ?? 0;

  return (
    <div className="mx-3 mt-3 rounded-lg border border-neutral-200 bg-white px-3 py-2.5 dark:border-neutral-800 dark:bg-neutral-900">
      <p className="truncate text-sm font-medium" title={state.title}>
        {state.title || 'YouTube 视频'}
      </p>
      <p className="mt-0.5 text-xs text-neutral-400">
        yt-dlp 解析 · 视频+音频自动合流
        {state.duration > 0 && ` · 时长 ${fmtDuration(state.duration)}`}
      </p>
      <div className="mt-2 grid grid-cols-2 gap-2">
        <label className="min-w-0">
          <span className="mb-1 block text-xs text-neutral-400">画质</span>
          <select
            value={activeQuality}
            disabled={audioOnly}
            onChange={(e) => setQualityId(Number(e.target.value))}
            className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1.5 text-xs disabled:opacity-40 dark:border-neutral-700 dark:bg-neutral-800"
          >
            {state.qualities.map((q) => (
              <option key={q.id} value={q.id}>
                {q.desc}
                {q.size > 0 && ` · ${fmtSize(q.size)}`}
              </option>
            ))}
          </select>
        </label>
        <label className="min-w-0">
          <span className="mb-1 block text-xs text-neutral-400">格式</span>
          <select
            value={format}
            onChange={(e) => setFormat(e.target.value)}
            className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1.5 text-xs dark:border-neutral-700 dark:bg-neutral-800"
          >
            {FORMATS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}
              </option>
            ))}
          </select>
        </label>
      </div>
      <button
        type="button"
        disabled={sending}
        onClick={() => void send()}
        className="mt-2.5 flex w-full items-center justify-center gap-1.5 rounded-md bg-sky-500 py-1.5 text-xs font-medium text-white transition hover:bg-sky-600 disabled:opacity-50"
      >
        {sending ? (
          <Loader2 className="size-3.5 animate-spin" />
        ) : (
          <Download className="size-3.5" />
        )}
        发送到 Knot
      </button>
      {result && (
        <p
          className={`mt-1.5 truncate text-xs ${result.ok ? 'text-emerald-500' : 'text-red-500'}`}
          title={result.message}
        >
          {result.message}
        </p>
      )}
    </div>
  );
}
