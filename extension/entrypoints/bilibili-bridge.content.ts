// B 站桥接（ISOLATED world）：MAIN world 内容脚本没有扩展 API，把
// window.postMessage 的播放信息转发给背景页。只认自家标记的消息。
import { browser } from 'wxt/browser';

export default defineContentScript({
  matches: ['*://*.bilibili.com/video/*'],
  runAt: 'document_start',
  main() {
    window.addEventListener('message', (event: MessageEvent) => {
      if (event.source !== window) return;
      const d = event.data as { source?: string; type?: string; payload?: unknown } | null;
      if (!d || d.source !== 'knot-ext' || d.type !== 'bili' || !d.payload) return;
      void browser.runtime
        .sendMessage({ type: 'biliPayload', payload: d.payload })
        .catch(() => {
          // 扩展刚重载时发送会失败，静默即可；下轮提取会再推。
        });
    });
  },
});
