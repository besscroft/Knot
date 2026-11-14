import { useState } from 'react';
import { Download } from 'lucide-react';

// 保存文件确认框：逐个确认待下载项的文件名（与应用内嗅探浮窗的
// DownloadConfirm 同一口径）。预填建议名；留空发送 = 由 Knot 从链接
// 自动推断；URL 以提示行完整展示供核对。

interface Props {
  /** 当前项地址（完整展示）。 */
  url: string;
  /** 预填建议名（嗅探名/站点标题，可含扩展名）。 */
  suggested: string;
  /** 含当前项的剩余待确认数。 */
  remaining: number;
  onConfirm: (name: string) => void;
  /** 跳过当前项（不下载）。 */
  onCancel: () => void;
}

export default function SaveConfirm({ url, suggested, remaining, onConfirm, onCancel }: Props) {
  const [name, setName] = useState(suggested);

  return (
    <div className="absolute inset-0 z-10 flex flex-col bg-white/95 backdrop-blur-sm dark:bg-neutral-950/95">
      <div className="border-b border-neutral-200 px-3 py-2.5 text-sm font-semibold dark:border-neutral-800">
        {remaining > 1 ? `保存文件（还有 ${remaining - 1} 个待确认）` : '保存文件'}
      </div>
      <div className="flex min-h-0 flex-1 flex-col justify-center gap-3 px-3 py-2">
        <label className="block">
          <span className="mb-1 block text-xs text-neutral-400">文件名</span>
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onFocus={(e) => e.target.select()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onConfirm(name.trim());
            }}
            placeholder="留空则从链接自动推断"
            className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
          />
        </label>
        <div className="truncate text-xs text-neutral-400" title={url}>
          {url}
        </div>
        <div className="flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md px-3 py-1.5 text-xs text-neutral-500 transition hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
          >
            跳过
          </button>
          <button
            type="button"
            onClick={() => onConfirm(name.trim())}
            className="flex items-center gap-1.5 rounded-md bg-sky-500 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-sky-600"
          >
            <Download className="size-3.5" />
            下载
          </button>
        </div>
      </div>
    </div>
  );
}
