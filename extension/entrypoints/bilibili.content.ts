// B 站播放信息提取（MAIN world）：读页面内的 __playinfo__ 与
// __INITIAL_STATE__，与后端 biliEvalCode（internal/service/bilibili.go）
// 同款逻辑，避免两处口径漂移。MAIN world 没有扩展 API，提取结果经
// window.postMessage 交给 bilibili-bridge（ISOLATED）转发给背景页。

export default defineContentScript({
  matches: ['*://*.bilibili.com/video/*'],
  world: 'MAIN',
  runAt: 'document_idle',
  main() {
    const SOURCE = 'knot-ext';

    function streams(list: Record<string, unknown>[]): Record<string, unknown>[] {
      return (list || []).map((s) => ({
        id: s.id,
        codecs: s.codecs || '',
        bandwidth: s.bandwidth || 0,
        base: s.baseUrl || s.base_url || '',
      }));
    }

    function extract(): Record<string, unknown> | null {
      const w = window as unknown as Record<string, unknown>;
      const pi = w.__playinfo__ as Record<string, unknown> | null;
      const st = w.__INITIAL_STATE__ as Record<string, unknown> | null;
      if (!pi) return null;
      const dash = pi.dash as Record<string, unknown> | undefined;
      const videoData = st?.videoData as Record<string, unknown> | undefined;
      const pages = (videoData?.pages as Record<string, unknown>[] | undefined) || [];
      const q = new URLSearchParams(location.search);
      const p = parseInt(q.get('p') || '1', 10) || 1;
      const cur = pages[p - 1] || {};
      let title = (videoData?.title as string) || '';
      if (!title) title = document.title.replace(/_哔哩哔哩.*$/, '');
      return {
        ok: true,
        dash: !!dash,
        timelength: pi.timelength || 0,
        title,
        page: p,
        pages: pages.length || 1,
        part: cur.part || '',
        cid: cur.cid || videoData?.cid || 0,
        bvid: videoData?.bvid || '',
        aid: videoData?.aid || 0,
        acceptIds: (pi.accept_quality as unknown[]) || [],
        acceptDesc: (pi.accept_description as unknown[]) || [],
        videos: dash ? streams((dash.video as Record<string, unknown>[]) || []) : [],
        audios: dash ? streams((dash.audio as Record<string, unknown>[]) || []) : [],
        durl: ((pi.durl as Record<string, unknown>[]) || []).map((d) => ({
          url: d.url || '',
          size: d.size || 0,
        })),
      };
    }

    /** 有可用流（DASH 分轨或旧版 durl）才算有效，空壳 playinfo 不推送。 */
    function usable(p: Record<string, unknown>): boolean {
      if (!p.ok) return false;
      if (p.dash) {
        return (
          (p.videos as unknown[]).length > 0 || (p.audios as unknown[]).length > 0
        );
      }
      return (p.durl as unknown[]).length > 0;
    }

    let lastKey = '';

    function push(): void {
      const p = extract();
      if (!p || !usable(p)) return;
      const key = `${p.bvid}:${p.cid}:${p.timelength}`;
      if (key === lastKey) return; // 未变化不重复推送
      lastKey = key;
      window.postMessage({ source: SOURCE, type: 'bili', payload: p }, '*');
    }

    // playinfo 由服务端渲染，但部分页面播放器异步填充，轮询兜底（约 10 分钟）。
    let ticks = 0;
    const timer = setInterval(() => {
      push();
      if (++ticks >= 240) clearInterval(timer);
    }, 2500);

    // B 站是 SPA：监听路由变化，切视频/分 P 后重新提取。
    for (const name of ['pushState', 'replaceState'] as const) {
      const orig = history[name].bind(history);
      history[name] = function (this: History, ...args: unknown[]) {
        const ret = (orig as (...a: unknown[]) => void)(...args);
        queueMicrotask(push);
        return ret;
      };
    }
    window.addEventListener('popstate', push);
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) push();
    });
    push();
  },
});
