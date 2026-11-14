import type { BiliPayload, BiliQuality, BiliStream } from './types';

// 移植自后端 biliInfoFrom / pickBiliAudio / biliCodecRank / biliQualityDesc
// （internal/service/bilibili.go），仅用于 popup 展示画质选项；实际挑流
// 与下载仍由 Knot 后端的同款逻辑完成。

const QUALITY_NAMES: Record<number, string> = {
  127: '8K 超高清',
  126: '杜比视界',
  125: 'HDR 真彩',
  120: '4K 超清',
  116: '1080P 60帧',
  112: '1080P 高码率',
  100: '智能修复',
  80: '1080P 高清',
  74: '720P 60帧',
  64: '720P 高清',
  32: '480P 清晰',
  16: '360P 流畅',
  6: '240P',
  5: '144P',
};

function qualityDesc(id: number, ids: number[], descs: string[]): string {
  const i = ids.indexOf(id);
  const desc = i >= 0 ? descs[i] : undefined;
  if (desc && desc.trim()) return desc;
  return QUALITY_NAMES[id] ?? `画质 ${id}`;
}

/** 编码兼容性排序：avc 优先，其次 hevc、av1。 */
function codecRank(codec: string): number {
  const c = codec.toLowerCase();
  if (c.includes('avc')) return 0;
  if (c.includes('hev') || c.includes('hvc')) return 1;
  if (c.includes('av01')) return 2;
  return 3;
}

/** 取画质号最高的常规音轨（杜比/Hi-Res 在单独字段，不取）。 */
export function pickAudio(p: BiliPayload): BiliStream | null {
  let best: BiliStream | null = null;
  for (const a of p.audios) {
    if (!a.base) continue;
    if (!best || a.id > best.id) best = a;
  }
  return best;
}

/**
 * 画质选项：按画质号降序，同画质多编码时展示兼容性最好的编码；
 * 大小 = (视频码率 + 音轨码率) / 8 × 时长。
 */
export function biliQualities(p: BiliPayload): BiliQuality[] {
  const sec = p.timelength > 0 ? p.timelength / 1000 : 0;
  const audio = pickAudio(p);
  const audioBandwidth = audio?.bandwidth ?? 0;

  const picked = new Map<number, BiliQuality>();
  for (const v of p.videos) {
    if (!v.base) continue;
    const ex = picked.get(v.id);
    if (ex) {
      if (codecRank(v.codecs) < codecRank(ex.codecs)) ex.codecs = v.codecs;
      continue;
    }
    picked.set(v.id, {
      id: v.id,
      desc: qualityDesc(v.id, p.acceptIds, p.acceptDesc),
      codecs: v.codecs,
      size: sec > 0 ? Math.round(((v.bandwidth + audioBandwidth) / 8) * sec) : 0,
    });
  }
  return [...picked.values()].sort((a, b) => b.id - a.id);
}
