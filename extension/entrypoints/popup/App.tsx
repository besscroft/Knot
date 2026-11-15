import { useMemo, useRef, useState } from 'react';
import { Settings } from 'lucide-react';
import BiliCard from './components/BiliCard';
import ItemRow from './components/ItemRow';
import SaveConfirm from './components/SaveConfirm';
import YouTubeCard from './components/YouTubeCard';
import type { BgRequest, ExtSettings, SendItem, SendResult, SniffItem, TabData } from '@/lib/types';

type Filter = 'all' | 'video' | 'audio' | 'hls' | 'other';

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'video', label: '视频' },
  { key: 'audio', label: '音频' },
  { key: 'hls', label: 'HLS' },
  { key: 'other', label: '其他' },
];

const FILTER_MATCH: Record<Filter, (t: SniffItem) => boolean> = {
  all: () => true,
  video: (i) => i.type === 'video',
  audio: (i) => i.type === 'audio',
  hls: (i) => i.type === 'hls',
  other: (i) => i.type === 'image' || i.type === 'archive' || i.type === 'document',
};

function send<T>(req: BgRequest): Promise<T> {
  return browser.runtime.sendMessage(req) as Promise<T>;
}

export default function App() {
  const [tabId, setTabId] = useState(0);
  // 实时标签页地址：YouTube 是 SPA，切视频不触发顶层导航，TabData.pageUrl
  // 会滞后，站点卡片（YouTube）一律以这里为准。
  const [tabUrl, setTabUrl] = useState('');
  const [data, setData] = useState<TabData | null>(null);
  const [settings, setSettings] = useState<ExtSettings | null>(null);
  const [online, setOnline] = useState<boolean | null>(null);
  const [filter, setFilter] = useState<Filter>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [status, setStatus] = useState<Record<string, 'sending' | 'ok' | 'error'>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  // 文件名确认队列：下载前逐个询问文件名（与应用内嗅探浮窗同一交互），
  // 确认/跳过到队尾后一次性发送。
  const [queue, setQueue] = useState<SniffItem[]>([]);
  const confirmedRef = useRef<SendItem[]>([]);

  useEffect(() => {
    void (async () => {
      const [tab] = await browser.tabs.query({ active: true, currentWindow: true });
      if (!tab?.id) return;
      setTabId(tab.id);
      setTabUrl(tab.url ?? '');
      void send({ type: 'popupOpened', tabId: tab.id }).catch(() => {});
      const res = await send<{ data: TabData; settings: ExtSettings }>({
        type: 'tabData',
        tabId: tab.id,
      });
      setData(res.data);
      setSettings(res.settings);
      try {
        await send({ type: 'ping' });
        setOnline(true);
      } catch {
        setOnline(false);
      }
    })();
  }, []);

  // 标签页数据（嗅探结果/B 站播放信息）落盘后实时刷新：内容脚本是异步
  // 上报的，popup 打开时可能还没到，只取一次会一直停在等待态。
  useEffect(() => {
    if (!tabId) return;
    const key = `tab:${tabId}`;
    const onChanged = (
      changes: Record<string, { newValue?: unknown; oldValue?: unknown }>,
      area: string,
    ): void => {
      if (area !== 'session' || !changes[key]) return;
      const next = changes[key].newValue as TabData | undefined;
      if (next) setData(next);
    };
    browser.storage.onChanged.addListener(onChanged);
    return () => browser.storage.onChanged.removeListener(onChanged);
  }, [tabId]);

  const visible = useMemo(() => (data?.sniffs ?? []).filter(FILTER_MATCH[filter]), [data, filter]);
  const picked = useMemo(() => data?.sniffs.filter((s) => selected.has(s.url)) ?? [], [data, selected]);
  const confirming = queue[0];

  function toggle(url: string): void {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(url)) next.delete(url);
      else next.add(url);
      return next;
    });
  }

  /** 开始逐个确认所选资源。 */
  function startConfirm(): void {
    if (!picked.length) return;
    confirmedRef.current = [];
    setQueue(picked);
  }

  /** 队列推进一格；走完即把确认结果一次性发送。 */
  function advance(rest: SniffItem[]): void {
    setQueue(rest);
    if (rest.length === 0) void sendConfirmed(confirmedRef.current);
  }

  function confirmCurrent(name: string): void {
    const cur = queue[0];
    if (cur) confirmedRef.current.push({ url: cur.url, name });
    advance(queue.slice(1));
  }

  function skipCurrent(): void {
    advance(queue.slice(1));
  }

  async function sendConfirmed(items: SendItem[]): Promise<void> {
    if (!items.length) return;
    setStatus(Object.fromEntries(items.map((i) => [i.url, 'sending' as const])));
    try {
      const results = await send<SendResult[]>({ type: 'send', tabId, items });
      const nextStatus: Record<string, 'sending' | 'ok' | 'error'> = {};
      const nextErrors: Record<string, string> = {};
      let failed = 0;
      for (const r of results) {
        nextStatus[r.url] = r.ok ? 'ok' : 'error';
        if (!r.ok) {
          nextErrors[r.url] = r.error ?? '发送失败';
          failed++;
        }
      }
      setStatus((prev) => ({ ...prev, ...nextStatus }));
      setErrors(nextErrors);
      // 成功的移出选中，只留失败的便于重试。
      setSelected(new Set(results.filter((r) => !r.ok).map((r) => r.url)));
      if (results.length && failed === results.length) setOnline(false);
    } catch {
      setOnline(false);
      setStatus({});
    }
  }

  return (
    <div className="relative flex h-full flex-col">
      {/* 头部：连接状态 + 设置入口 */}
      <header className="flex items-center gap-2 border-b border-neutral-200 px-3 py-2.5 dark:border-neutral-800">
        <span
          className={`size-2 shrink-0 rounded-full ${
            online === true
              ? 'bg-emerald-500'
              : online === false
                ? 'bg-red-500'
                : 'bg-neutral-300 dark:bg-neutral-700'
          }`}
        />
        <span className="font-semibold">Knot</span>
        <span className="min-w-0 flex-1 truncate text-xs text-neutral-400">
          {settings?.serverBase.replace(/^https?:\/\//, '') ?? ''}
        </span>
        <button
          type="button"
          onClick={() => void browser.runtime.openOptionsPage()}
          className="rounded-md p-1.5 text-neutral-400 transition hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
          title="设置"
        >
          <Settings className="size-4" />
        </button>
      </header>

      {/* 主体：站点专用卡片（B 站 / YouTube，各自按页面地址显示） */}
      {data && <BiliCard data={data} tabId={tabId} tabUrl={tabUrl} />}
      {data && <YouTubeCard data={data} tabId={tabId} tabUrl={tabUrl} />}

      {/* 过滤 chips */}
      <div className="flex gap-1.5 px-3 pt-2.5">
        {FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            onClick={() => setFilter(f.key)}
            className={`rounded-full px-2.5 py-1 text-xs transition ${
              filter === f.key
                ? 'bg-sky-500 font-medium text-white'
                : 'bg-neutral-200/70 text-neutral-500 hover:bg-neutral-300/70 dark:bg-neutral-800 dark:text-neutral-400 dark:hover:bg-neutral-700'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      {/* 列表 */}
      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
        {online === false && (
          <div className="mb-2 rounded-lg border border-red-300/60 bg-red-50 px-3 py-2 text-xs text-red-600 dark:border-red-900 dark:bg-red-950/50 dark:text-red-400">
            无法连接 Knot，请确认应用已运行且地址正确
          </div>
        )}
        {visible.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-sm text-neutral-400">
            <span className="text-2xl">🔎</span>
            <p>尚未嗅探到{filter === 'all' ? '' : FILTERS.find((f) => f.key === filter)?.label}资源</p>
            <p className="text-xs">播放视频或刷新页面后重试</p>
          </div>
        ) : (
          <ul className="divide-y divide-neutral-200/70 dark:divide-neutral-800/70">
            {visible.map((item) => (
              <ItemRow
                key={item.url}
                item={item}
                checked={selected.has(item.url)}
                status={status[item.url]}
                error={errors[item.url]}
                onToggle={() => toggle(item.url)}
              />
            ))}
          </ul>
        )}
      </div>

      {/* 底部操作 */}
      <footer className="border-t border-neutral-200 px-3 py-2.5 dark:border-neutral-800">
        <button
          type="button"
          disabled={picked.length === 0 || online === false}
          onClick={startConfirm}
          className="w-full rounded-lg bg-sky-500 py-2 text-sm font-medium text-white transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-40"
        >
          下载所选{picked.length > 0 ? `（${picked.length}）` : ''}
        </button>
      </footer>

      {/* 文件名确认队列：逐个询问后一次性发送 */}
      {confirming && (
        <SaveConfirm
          key={confirming.url}
          url={confirming.url}
          suggested={confirming.name}
          remaining={queue.length}
          onConfirm={confirmCurrent}
          onCancel={skipCurrent}
        />
      )}
    </div>
  );
}
