import { useEffect, useState } from "react";
import { App, type Settings as SettingsData } from "../mygo";
import { Button, Input } from "../components/ui";

/** 设置页：下载目录、分片数、并发数。 */
export function SettingsView() {
  const [data, setData] = useState<SettingsData | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
    setSaving(true);
    setError(null);
    try {
      await App.saveSettings(data);
      setDirty(false);
      setSaved(true);
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
