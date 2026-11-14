import {
  Check,
  Film,
  FileArchive,
  FileText,
  Image,
  Loader2,
  Music,
  Radio,
  Send,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react';
import { fmtSize } from '@/lib/format';
import type { SniffItem } from '@/lib/types';

const TYPE_META: Record<SniffItem['type'], { label: string; cls: string; Icon: LucideIcon }> = {
  hls: { label: 'HLS', cls: 'text-sky-500', Icon: Radio },
  video: { label: '视频', cls: 'text-violet-500', Icon: Film },
  audio: { label: '音频', cls: 'text-emerald-500', Icon: Music },
  image: { label: '图片', cls: 'text-amber-500', Icon: Image },
  archive: { label: '压缩包', cls: 'text-orange-500', Icon: FileArchive },
  document: { label: '文档', cls: 'text-neutral-400', Icon: FileText },
};

interface Props {
  item: SniffItem;
  checked: boolean;
  status?: 'sending' | 'ok' | 'error';
  error?: string;
  onToggle: () => void;
}

export default function ItemRow({ item, checked, status, error, onToggle }: Props) {
  const meta = TYPE_META[item.type];
  return (
    <li className="flex items-center gap-2 py-1.5">
      <input
        type="checkbox"
        checked={checked}
        onChange={onToggle}
        className="size-4 shrink-0 accent-sky-500"
      />
      <meta.Icon className={`size-4 shrink-0 ${meta.cls}`} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm" title={item.url}>
          {item.name || item.url}
        </p>
        <p className="text-xs text-neutral-400">
          {meta.label}
          {item.size > 0 && ` · ${fmtSize(item.size)}`}
        </p>
        {status === 'error' && error && (
          <p className="truncate text-xs text-red-500" title={error}>
            {error}
          </p>
        )}
      </div>
      {status === 'sending' ? (
        <Loader2 className="size-4 shrink-0 animate-spin text-sky-500" />
      ) : status === 'ok' ? (
        <Check className="size-4 shrink-0 text-emerald-500" />
      ) : status === 'error' ? (
        <TriangleAlert className="size-4 shrink-0 text-red-500" />
      ) : (
        <Send className="size-3.5 shrink-0 text-neutral-300 dark:text-neutral-600" />
      )}
    </li>
  );
}
