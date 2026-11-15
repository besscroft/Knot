import { useEffect, useMemo, useState } from 'react';
import { Download, Loader2, RefreshCw, Tv } from 'lucide-react';
import { biliQualities } from '@/lib/bili';
import { fmtSize } from '@/lib/format';
import type { TabData } from '@/lib/types';

// B 站卡片：与应用内 SniffPanel 的 BilibiliDialog 同一口径 —— DASH 分轨
// 提供清晰度选择（默认最高），音频格式只提取音轨；旧版 durl 单文件直发。
const FORMATS = [
  { value: 'mp4', label: 'MP4 视频' },
  { value: 'mkv', label: 'MKV 视频' },
  { value: 'm4a', label: 'M4A 仅音频' },
  { value: 'mp3', label: 'MP3 仅音频' },
];

interface Props {
  data: TabData;
  tabId: number;
  /** 弹窗打开时的实时标签页地址（SPA 站点 pageUrl 会滞后，识别以它为准）。 */
  tabUrl?: string;
}

export default function BiliCard({ data, tabId, tabUrl }: Props) {
  const bili = data.bili;
  const isBiliPage = /^https?:\/\/(www|m)\.bilibili\.com\/video\//.test(tabUrl || data.pageUrl);
  const qualities = useMemo(() => (bili ? biliQualities(bili) : []), [bili]);
  const [qualityId, setQualityId] = useState<number | null>(null);
  const [format, setFormat] = useState('mp4');
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);

  const activeQuality = qualityId ?? qualities[0]?.id ?? 0;
  const audioOnly = format === 'm4a' || format === 'mp3';
  const title = bili?.title
    ? bili.pages > 1 && bili.part
      ? `${bili.title} · ${bili.part}`
      : bili.title
    : '';
  // 文件名预填视频标题（payload 可能二次推送补全，未手动编辑前跟随）；
  // 留空发送 = 由 Knot 生成默认名。
  const [name, setName] = useState('');
  const [nameTouched, setNameTouched] = useState(false);
  useEffect(() => {
    if (!nameTouched) setName(title);
  }, [title]);

  // 等待态手动重试：通知页面内容脚本立即强拉一次播放信息。
  const [refreshing, setRefreshing] = useState(false);
  const [refreshErr, setRefreshErr] = useState('');
  async function refresh(): Promise<void> {
    setRefreshing(true);
    setRefreshErr('');
    try {
      const res = (await browser.runtime.sendMessage({
        type: 'biliRefresh',
        tabId,
      })) as { ok: boolean; error?: string } | undefined;
      if (!res?.ok) setRefreshErr(res?.error || '刷新失败，请稍后重试');
    } catch {
      setRefreshErr('刷新失败，请刷新 B 站页面后重试');
    } finally {
      setRefreshing(false);
    }
  }

  if (!bili && isBiliPage) {
    return (
      <div className="mx-3 mt-3 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2.5 text-xs text-sky-700 dark:border-sky-900 dark:bg-sky-950/40 dark:text-sky-400">
        <div className="flex items-center gap-2">
          <Tv className="size-4 shrink-0" />
          <span>已识别 B 站视频页，正在获取播放信息…</span>
        </div>
        {refreshErr && <p className="mt-1.5 text-red-500">{refreshErr}</p>}
        <button
          type="button"
          disabled={refreshing}
          onClick={() => void refresh()}
          className="mt-2 flex items-center gap-1.5 rounded-md border border-sky-300 px-2 py-1 font-medium transition hover:bg-sky-100 disabled:opacity-50 dark:border-sky-800 dark:hover:bg-sky-900/40"
        >
          {refreshing ? (
            <Loader2 className="size-3 animate-spin" />
          ) : (
            <RefreshCw className="size-3" />
          )}
          重新获取
        </button>
      </div>
    );
  }
  if (!bili) return null;

  async function send(): Promise<void> {
    setSending(true);
    setResult(null);
    try {
      const res = (await browser.runtime.sendMessage({
        type: 'sendBili',
        tabId,
        qualityId: activeQuality,
        format,
        name: name.trim(),
      })) as { ok: boolean; error?: string };
      setResult({ ok: res?.ok ?? false, message: res?.ok ? '已发送到 Knot' : res?.error ?? '发送失败' });
    } catch {
      setResult({ ok: false, message: '发送失败，请重试' });
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="mx-3 mt-3 rounded-lg border border-neutral-200 bg-white px-3 py-2.5 dark:border-neutral-800 dark:bg-neutral-900">
      <p className="truncate text-sm font-medium" title={title}>
        {title || 'B 站视频'}
      </p>
      {!bili.dash && (
        <p className="mt-0.5 text-xs text-neutral-400">旧版单文件模式</p>
      )}
      {bili.dash && qualities.length > 0 && (
        <div className="mt-2 grid grid-cols-2 gap-2">
          <label className="min-w-0">
            <span className="mb-1 block text-xs text-neutral-400">清晰度</span>
            <select
              value={activeQuality}
              disabled={audioOnly}
              onChange={(e) => setQualityId(Number(e.target.value))}
              className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1.5 text-xs disabled:opacity-40 dark:border-neutral-700 dark:bg-neutral-800"
            >
              {qualities.map((q) => (
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
      )}
      <label className="mt-2 block">
        <span className="mb-1 block text-xs text-neutral-400">文件名（留空用视频标题）</span>
        <input
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setNameTouched(true);
          }}
          onFocus={(e) => e.target.select()}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !sending) void send();
          }}
          placeholder="由 Knot 自动命名"
          className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1.5 text-xs dark:border-neutral-700 dark:bg-neutral-800"
        />
      </label>
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
