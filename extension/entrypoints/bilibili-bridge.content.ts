// B 站桥接（ISOLATED world）：MAIN world 内容脚本没有扩展 API，把
// window.postMessage 的播放信息转发给背景页；背景页（弹窗「重新获取」）
// 的刷新指令也经这里转发回 MAIN world。只认自家标记的消息。matches 覆盖
// 整个 bilibili.com（B 站 SPA 内部导航不会重新注入脚本）。
import { browser } from 'wxt/browser';

export default defineContentScript({
  matches: ['*://*.bilibili.com/*'],
  runAt: 'document_start',
  main() {
    window.addEventListener('message', (event: MessageEvent) => {
      if (event.source !== window) return;
      const d = event.data as { source?: string; type?: string; payload?: unknown } | null;
      if (!d || d.source !== 'knot-ext' || d.type !== 'bili' || !d.payload) return;
      void browser.runtime
        .sendMessage({ type: 'biliPayload', payload: d.payload })
        .catch(() => {
          // 扩展刚重载时发送会失败，静默即可；主动拉取下一轮会再推。
        });
    });

    // 背景页 tabs.sendMessage 广播 → 弹窗「重新获取」→ MAIN world 强拉。
    browser.runtime.onMessage.addListener((msg: { type?: string }) => {
      if (msg?.type === 'knotBiliRefresh') {
        window.postMessage({ source: 'knot-ext', type: 'refresh' }, '*');
      }
    });
  },
});
