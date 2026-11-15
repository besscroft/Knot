import { classify, urlName } from '@/lib/classify';
import * as knot from '@/lib/knot';
import { dropTabData, loadTabData, resetTabData, saveTabData } from '@/lib/storage';
import type { BgRequest, SendResult, SniffItem, TabData, YouTubeState } from '@/lib/types';
import { isYouTubeVideoPage } from '@/lib/youtube';
import { browser, type Browser } from 'wxt/browser';

// 背景（MV3 Service Worker）：嗅探 + 消息路由 + 右键菜单。
// 嗅探口径对齐 Knot 应用内浏览器：webRequest 观察响应，按扩展名与
// Content-Type 分类，按标签页去重保存（cap 200），徽标提示有新资源。

const MAX_SNIFFS = 200;

/** 图标路径（@wxt-dev/auto-icons 生成的产物）。 */
const ICON_URL = '/icon/128.png' as Parameters<typeof browser.runtime.getURL>[0];

export default defineBackground(() => {
  // ---- 嗅探 ------------------------------------------------------------

  // 顶层导航：重置该标签页状态（新页面的嗅探结果与 Referer 全部刷新）。
  // YouTube 视频页另亮徽标：播放媒体走 SABR/UMP，嗅探层看不见，下载入口
  // 是弹窗的专用卡片（打开后按需解析）。
  browser.webRequest.onBeforeRequest.addListener(
    (details) => {
      if (details.type !== 'main_frame' || details.tabId < 0) return;
      void resetTabData(details.tabId, details.url).then(() => {
        if (!isYouTubeVideoPage(details.url)) {
          browser.action.setBadgeText({ tabId: details.tabId, text: '' });
          return;
        }
        void browser.action.setBadgeText({ tabId: details.tabId, text: '•' });
        void browser.action.setBadgeBackgroundColor({ tabId: details.tabId, color: '#0ea5e9' });
      });
    },
    { urls: ['<all_urls>'] },
  );

  browser.webRequest.onCompleted.addListener(
    (details) => {
      void onSniffed(details);
    },
    { urls: ['<all_urls>'] },
    ['responseHeaders'],
  );

  async function onSniffed(details: {
    tabId: number;
    url: string;
    statusCode: number;
    initiator?: string;
    responseHeaders?: { name: string; value?: string }[];
  }): Promise<void> {
    if (details.tabId < 0) return; // 无标签页的请求（预取、SW 等）
    if (details.statusCode < 200 || details.statusCode >= 300) return;
    if (details.initiator?.startsWith('chrome-extension://')) return; // 插件自身
    const header = (name: string) =>
      details.responseHeaders?.find((h) => h.name.toLowerCase() === name)?.value ?? '';
    const contentType = header('content-type');
    const contentLength = Number.parseInt(header('content-length'), 10) || 0;
    const type = classify(details.url, contentType, contentLength);
    if (!type) return;

    const data = await loadTabData(details.tabId);
    if (data.sniffs.some((s) => s.url === details.url)) return; // 已捕获
    const item: SniffItem = {
      url: details.url,
      name: urlName(details.url),
      type,
      size: contentLength,
      time: Date.now(),
    };
    data.sniffs.unshift(item);
    if (data.sniffs.length > MAX_SNIFFS) data.sniffs.length = MAX_SNIFFS;
    saveTabData(details.tabId);
    await browser.action.setBadgeText({ tabId: details.tabId, text: '•' });
    await browser.action.setBadgeBackgroundColor({ tabId: details.tabId, color: '#0ea5e9' });
  }

  // ---- 发送上下文 -------------------------------------------------------

  /** 目标 URL 的 Cookie（防盗链上下文，与应用内下载路径对齐）。 */
  async function cookieString(url: string): Promise<string> {
    try {
      const cookies = await browser.cookies.getAll({ url });
      return cookies.map((c) => `${c.name}=${c.value}`).join('; ');
    } catch {
      return '';
    }
  }

  /** 标签页实时地址：SPA 站点切页不触发顶层导航，pageUrl 会滞后。 */
  async function liveTabUrl(tabId: number, fallback: string): Promise<string> {
    try {
      const tab = await browser.tabs.get(tabId);
      return tab.url || fallback;
    } catch {
      return fallback;
    }
  }

  /** 右键菜单发送无需打开 popup，用系统通知反馈结果。 */
  function notify(title: string, message: string): void {
    void browser.notifications.create({
      type: 'basic',
      iconUrl: browser.runtime.getURL(ICON_URL),
      title,
      message,
    });
  }

  // ---- YouTube ---------------------------------------------------------

  // 解析去重：弹窗反复开关会重复触发，同标签页合并为同一次请求。
  const ytResolving = new Map<number, Promise<YouTubeState>>();

  function resolveYouTubeForTab(tabId: number, url: string, force: boolean): Promise<YouTubeState> {
    const hit = ytResolving.get(tabId);
    if (hit) return hit;
    const p = doResolveYouTube(tabId, url, force).finally(() => ytResolving.delete(tabId));
    ytResolving.set(tabId, p);
    return p;
  }

  /** 调 /api/youtube/resolve 并把结果（含失败）落到标签页状态。
   * force=false 时同地址直接复用已有结果，避免重复跑 yt-dlp。 */
  async function doResolveYouTube(tabId: number, url: string, force: boolean): Promise<YouTubeState> {
    const data = await loadTabData(tabId);
    if (!force && data.youtube?.url === url) return data.youtube;
    const state: YouTubeState = { url, ok: false, title: '', duration: 0, qualities: [], error: '' };
    try {
      const settings = await knot.loadSettings();
      const info = await knot.resolveYouTube(settings.serverBase, url);
      state.ok = true;
      state.title = info.title;
      state.duration = info.duration;
      state.qualities = info.qualities;
    } catch (err) {
      state.error = errMsg(err);
    }
    const latest = await loadTabData(tabId);
    latest.youtube = state;
    saveTabData(tabId);
    return state;
  }

  // ---- 消息路由 ---------------------------------------------------------

  browser.runtime.onMessage.addListener((msg: BgRequest, sender) =>
    handleMessage(msg, sender),
  );

  async function handleMessage(
    msg: BgRequest,
    sender: Browser.runtime.MessageSender,
  ): Promise<unknown> {
    switch (msg.type) {
      case 'tabData': {
        const [data, settings] = await Promise.all([loadTabData(msg.tabId), knot.loadSettings()]);
        return { data, settings };
      }
      case 'popupOpened': {
        await browser.action.setBadgeText({ tabId: msg.tabId, text: '' });
        return { ok: true };
      }
      case 'send': {
        const settings = await knot.loadSettings();
        const data = await loadTabData(msg.tabId);
        // B 站等 SPA 站点切页不触发顶层导航，TabData.pageUrl 会滞后，
        // 防盗链 Referer 一律以标签页实时地址为准。
        const pageUrl = await liveTabUrl(msg.tabId, data.pageUrl);
        const results: SendResult[] = [];
        for (const item of msg.items) {
          try {
            await knot.createOnKnot(settings.serverBase, {
              url: item.url,
              name: item.name,
              referer: pageUrl,
              cookie: await cookieString(item.url),
              userAgent: navigator.userAgent,
            });
            results.push({ url: item.url, ok: true });
          } catch (err) {
            results.push({ url: item.url, ok: false, error: errMsg(err) });
          }
        }
        return results;
      }
      case 'sendBili': {
        const data = await loadTabData(msg.tabId);
        if (!data.bili) return { ok: false, error: '未获取到 B 站播放信息，请刷新页面后重试' };
        try {
          const settings = await knot.loadSettings();
          const pageUrl = await liveTabUrl(msg.tabId, data.pageUrl);
          await knot.createOnKnot(settings.serverBase, {
            bili: data.bili,
            qualityId: msg.qualityId,
            format: msg.format,
            name: msg.name,
            referer: pageUrl,
            cookie: await cookieString(pageUrl),
            userAgent: navigator.userAgent,
          });
          return { ok: true };
        } catch (err) {
          return { ok: false, error: errMsg(err) };
        }
      }
      case 'biliRefresh': {
        // 弹窗「重新获取」→ 通知页面里的桥接脚本触发 MAIN world 强拉。
        try {
          await browser.tabs.sendMessage(msg.tabId, { type: 'knotBiliRefresh' });
          return { ok: true };
        } catch {
          return { ok: false, error: '无法通知页面脚本，请刷新 B 站页面后重试' };
        }
      }
      case 'biliPayload': {
        const tabId = sender.tab?.id;
        if (!tabId || tabId < 0 || !msg.payload?.ok) return { ok: false };
        const data: TabData = await loadTabData(tabId);
        data.bili = msg.payload;
        saveTabData(tabId);
        void browser.action.setBadgeText({ tabId, text: '•' });
        void browser.action.setBadgeBackgroundColor({ tabId, color: '#0ea5e9' });
        return { ok: true };
      }
      case 'resolveYouTube': {
        if (!isYouTubeVideoPage(msg.url)) {
          return { ok: false, error: '当前页面不是 YouTube 视频页' };
        }
        const state = await resolveYouTubeForTab(msg.tabId, msg.url, msg.force === true);
        return { ok: state.ok, state };
      }
      case 'sendYouTube': {
        if (!isYouTubeVideoPage(msg.url)) {
          return { ok: false, error: '不是 YouTube 视频页链接' };
        }
        try {
          const settings = await knot.loadSettings();
          await knot.createOnKnot(settings.serverBase, {
            url: msg.url,
            qualityId: msg.qualityId,
            format: msg.format,
            name: msg.name,
          });
          return { ok: true };
        } catch (err) {
          return { ok: false, error: errMsg(err) };
        }
      }
      case 'ping': {
        try {
          await knot.ping((await knot.loadSettings()).serverBase);
          return { ok: true };
        } catch (err) {
          return { ok: false, error: errMsg(err) };
        }
      }
    }
  }

  function errMsg(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  // ---- 右键菜单 ---------------------------------------------------------

  void browser.contextMenus.removeAll().then(() => {
    browser.contextMenus.create({
      id: 'knot-parent',
      title: '用 Knot 下载',
      contexts: ['link', 'video', 'audio', 'image'],
    });
    browser.contextMenus.create({
      id: 'knot-link',
      parentId: 'knot-parent',
      title: '下载链接目标',
      contexts: ['link'],
    });
    browser.contextMenus.create({
      id: 'knot-media',
      parentId: 'knot-parent',
      title: '下载此媒体',
      contexts: ['video', 'audio'],
    });
    browser.contextMenus.create({
      id: 'knot-image',
      parentId: 'knot-parent',
      title: '下载此图片',
      contexts: ['image'],
    });
  });

  browser.contextMenus.onClicked.addListener((info, tab) => {
    void onMenuClick(info, tab);
  });

  async function onMenuClick(
    info: Browser.contextMenus.OnClickData,
    tab: Browser.tabs.Tab | undefined,
  ): Promise<void> {
    if (!tab?.id || tab.id < 0) return;
    const target = info.linkUrl || info.srcUrl;
    if (!target) return;
    if (/^(blob:|data:)/i.test(target)) {
      notify('无法下载', 'blob/data 链接无法交给 Knot，请试试嗅探面板');
      return;
    }
    const data = await loadTabData(tab.id);
    try {
      const settings = await knot.loadSettings();
      await knot.createOnKnot(settings.serverBase, {
        url: target,
        referer: data.pageUrl || info.pageUrl || tab.url || '',
        cookie: await cookieString(target),
        userAgent: navigator.userAgent,
      });
      notify('已发送到 Knot', urlName(target));
    } catch (err) {
      notify('发送失败', errMsg(err));
    }
  }

  // ---- 生命周期 ---------------------------------------------------------

  browser.tabs.onRemoved.addListener((tabId) => dropTabData(tabId));
});
