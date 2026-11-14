import { useMemo, useState } from 'react';
import { Settings } from 'lucide-react';
import BiliCard from './components/BiliCard';
import ItemRow from './components/ItemRow';
import type { BgRequest, ExtSettings, SendResult, SniffItem, TabData } from '@/lib/types';

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
  const [data, setData] = useState<TabData | null>(null);
  const [settings, setSettings] = useState<ExtSettings | null>(null);
  const [online, setOnline] = useState<boolean | null>(null);
  const [filter, setFilter] = useState<Filter>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [status, setStatus] = useState<Record<string, 'sending' | 'ok' | 'error'>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    void (async () => {
      const [tab] = await browser.tabs.query({ active: true, currentWindow: true });
      if (!tab?.id) return;
      setTabId(tab.id);
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

  const visible = useMemo(() => (data?.sniffs ?? []).filter(FILTER_MATCH[filter]), [data, filter]);
  const picked = useMemo(() => data?.sniffs.filter((s) => selected.has(s.url)) ?? [], [data, selected]);

  function toggle(url: string): void {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(url)) next.delete(url);
      else next.add(url);
      return next;
    });
  }

  async function sendSelected(): Promise<void> {
    if (!picked.length) return;
    setStatus(Object.fromEntries(picked.map((i) => [i.url, 'sending' as const])));
    try {
      const results = await send<SendResult[]>({ type: 'send', tabId, items: picked });
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
    <div className="flex h-full flex-col">
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

      {/* 主体 */}
      {data && <BiliCard data={data} tabId={tabId} />}

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
          onClick={() => void sendSelected()}
          className="w-full rounded-lg bg-sky-500 py-2 text-sm font-medium text-white transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-40"
        >
          下载所选{picked.length > 0 ? `（${picked.length}）` : ''}
        </button>
      </footer>
    </div>
  );
}
