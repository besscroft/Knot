import type { SniffType } from './types';

// 分类口径对齐 Knot：扩展名清单取自 internal/service/tasks.go 的
// classifyMedia 与 browser.go 的 preload 脚本。
const EXT_TYPES: Record<string, SniffType> = {
  m3u8: 'hls',
  mp4: 'video',
  webm: 'video',
  mkv: 'video',
  mov: 'video',
  m4v: 'video',
  avi: 'video',
  ts: 'video',
  flv: 'video',
  mp3: 'audio',
  flac: 'audio',
  m4a: 'audio',
  aac: 'audio',
  wav: 'audio',
  ogg: 'audio',
  opus: 'audio',
  zip: 'archive',
  rar: 'archive',
  '7z': 'archive',
  tar: 'archive',
  gz: 'archive',
  pdf: 'document',
  doc: 'document',
  docx: 'document',
  xls: 'document',
  xlsx: 'document',
  ppt: 'document',
  pptx: 'document',
  txt: 'document',
  epub: 'document',
  jpg: 'image',
  jpeg: 'image',
  png: 'image',
  gif: 'image',
  webp: 'image',
  avif: 'image',
};

// 太小的多半是图标、封面或分段碎片，纯属噪音；HLS 清单很小，豁免。
const MIN_SIZE: Partial<Record<SniffType, number>> = {
  video: 128 << 10,
  audio: 128 << 10,
  archive: 32 << 10,
  document: 32 << 10,
  image: 100 << 10,
};

/** 截取 sep 之前的部分（不含 sep）。 */
function before(s: string, sep: string): string {
  const i = s.indexOf(sep);
  return i >= 0 ? s.slice(0, i) : s;
}

/** 取 URL 路径部分的扩展名（小写，不含点；无查询串干扰）。 */
export function urlExt(raw: string): string {
  let path = before(before(raw, '#'), '?');
  const slash = path.lastIndexOf('/');
  if (slash >= 0) path = path.slice(slash + 1);
  const dot = path.lastIndexOf('.');
  if (dot < 0) return '';
  return path.slice(dot + 1).toLowerCase();
}

/** 从 URL 推断展示/下载用的文件名（与 Knot 的推断方式一致）。 */
export function urlName(raw: string): string {
  let path = before(before(raw, '#'), '?');
  const slash = path.lastIndexOf('/');
  if (slash >= 0) path = path.slice(slash + 1);
  try {
    return decodeURIComponent(path) || path;
  } catch {
    return path;
  }
}

/**
 * 按扩展名 + Content-Type 分类；不属于可下载资源返回 null。
 * m4s 是 DASH 分段（Knot 暂不支持通用 DASH），直接忽略。
 */
export function classify(
  rawUrl: string,
  contentType: string,
  contentLength: number,
): SniffType | null {
  const ext = urlExt(rawUrl);
  if (ext === 'm4s') return null;

  const mt = contentType.toLowerCase();
  let type: SniffType | null = null;
  if (mt.includes('mpegurl')) {
    type = 'hls';
  } else if (mt.startsWith('video/') || mt.startsWith('audio/')) {
    // HLS 音视频也可能以 fmp4 直链出现，先看清单再落回扩展名判断。
    type = mt.startsWith('video/') ? 'video' : 'audio';
  }
  if (!type && ext) type = EXT_TYPES[ext] ?? null;
  if (!type) return null;

  const min = MIN_SIZE[type];
  if (type !== 'hls' && min && contentLength > 0 && contentLength < min) {
    return null;
  }
  return type;
}
