import { useEffect, useState } from 'react';
import { Loader2, PlugZap } from 'lucide-react';
import { DEFAULT_SERVER, loadSettings, normalizeBase, ping, saveSettings } from '@/lib/knot';

export default function Options() {
  const [base, setBase] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);

  useEffect(() => {
    void loadSettings().then((s) => {
      setBase(s.serverBase);
      setLoaded(true);
    });
  }, []);

  async function test(): Promise<void> {
    setTesting(true);
    setResult(null);
    try {
      await ping(normalizeBase(base));
      setResult({ ok: true, message: '连接成功' });
    } catch (err) {
      setResult({ ok: false, message: err instanceof Error ? err.message : String(err) });
    } finally {
      setTesting(false);
    }
  }

  async function save(): Promise<void> {
    await saveSettings({ serverBase: normalizeBase(base) });
    setResult({ ok: true, message: '已保存' });
    void test();
  }

  if (!loaded) return <div className="p-6 text-sm text-neutral-400">加载中…</div>;

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6 py-10">
      <h1 className="mb-1 text-lg font-semibold">Knot 插件设置</h1>
      <p className="mb-6 text-xs text-neutral-400">
        插件把嗅探到的资源发送给本机的 Knot 应用下载。
      </p>

      <label className="block">
        <span className="mb-1.5 block text-sm font-medium">Knot 服务器地址</span>
        <input
          type="text"
          value={base}
          onChange={(e) => setBase(e.target.value)}
          placeholder={DEFAULT_SERVER}
          className="w-full rounded-lg border border-neutral-300 bg-white px-3 py-2 text-sm outline-none focus:border-sky-500 focus:ring-2 focus:ring-sky-500/30 dark:border-neutral-700 dark:bg-neutral-900"
        />
      </label>

      <div className="mt-4 flex gap-2">
        <button
          type="button"
          disabled={testing}
          onClick={() => void test()}
          className="flex flex-1 items-center justify-center gap-1.5 rounded-lg border border-neutral-300 bg-white py-2 text-sm font-medium transition hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
        >
          {testing ? <Loader2 className="size-4 animate-spin" /> : <PlugZap className="size-4" />}
          测试连接
        </button>
        <button
          type="button"
          onClick={() => void save()}
          className="flex-1 rounded-lg bg-sky-500 py-2 text-sm font-medium text-white transition hover:bg-sky-600"
        >
          保存
        </button>
      </div>

      {result && (
        <p className={`mt-3 text-sm ${result.ok ? 'text-emerald-500' : 'text-red-500'}`}>
          {result.message}
        </p>
      )}

      <p className="mt-8 text-xs leading-relaxed text-neutral-400">
        默认地址为 {DEFAULT_SERVER}（Knot 应用内媒体服务器）。Knot
        需保持运行；插件发送任务时会自动携带当前页面的 Cookie 与 Referer 以规避防盗链。
      </p>
    </div>
  );
}
