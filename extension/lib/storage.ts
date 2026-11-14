import type { TabData } from './types';

// 按标签页的嗅探状态：chrome.storage.session（浏览器关闭即清空）。
// background 用内存 Map 做读写缓存，落盘去抖，SW 被回收后仍可恢复。

const tabKey = (tabId: number) => `tab:${tabId}`;
const cache = new Map<number, TabData>();
const pending = new Set<number>();
let flushTimer: ReturnType<typeof setTimeout> | null = null;

function emptyData(): TabData {
  return { pageUrl: '', sniffs: [], bili: null, youtube: null };
}

/** 读取标签页状态：先查缓存，未命中再查 session 存储。 */
export async function loadTabData(tabId: number): Promise<TabData> {
  const hit = cache.get(tabId);
  if (hit) return hit;
  const stored = (await browser.storage.session.get(tabKey(tabId)))[tabKey(tabId)] as
    | TabData
    | undefined;
  const data = stored ?? emptyData();
  cache.set(tabId, data);
  return data;
}

/** 重置标签页状态（页面导航时清空嗅探结果）。 */
export async function resetTabData(tabId: number, pageUrl: string): Promise<TabData> {
  const data = emptyData();
  data.pageUrl = pageUrl;
  cache.set(tabId, data);
  await browser.storage.session.set({ [tabKey(tabId)]: data });
  return data;
}

/** 丢弃标签页状态（标签页关闭时清理）。 */
export function dropTabData(tabId: number): void {
  cache.delete(tabId);
  pending.delete(tabId);
  void browser.storage.session.remove(tabKey(tabId));
}

/** 记录一次变更并去抖落盘。 */
export function saveTabData(tabId: number): void {
  pending.add(tabId);
  if (flushTimer) return;
  flushTimer = setTimeout(flush, 250);
}

async function flush(): Promise<void> {
  flushTimer = null;
  const ids = [...pending];
  pending.clear();
  for (const id of ids) {
    const data = cache.get(id);
    if (data) await browser.storage.session.set({ [tabKey(id)]: data });
  }
}
