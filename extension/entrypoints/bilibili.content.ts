// B 站播放信息提取（MAIN world）：与后端 biliEvalCode（internal/service/
// bilibili.go）同款逻辑，避免两处口径漂移。实测（2026-11）当前 B 站视频页
// 已不再提供 window.__playinfo__（播放器也不会异步回填，后端记录的「空壳」
// 事实上成了「不存在」），播放器改从 /x/player/wbi/playurl 接口拉清单，
// 所以主来源是劫持播放器的 playurl 响应——与实际播放完全一致，切画质/
// 分 P 都会重新请求——__playinfo__ 轮询兜底（旧页面/内嵌场景）。MAIN
// world 没有扩展 API，提取结果经 window.postMessage 交给
// bilibili-bridge（ISOLATED）转发给背景页。

export default defineContentScript({
  matches: ['*://*.bilibili.com/video/*'],
  world: 'MAIN',
  // document_start：钩子必须先于播放器启动，否则首个 playurl 请求会漏掉。
  runAt: 'document_start',
  main() {
    const SOURCE = 'knot-ext';

    /** playurl 接口地址（含 WBI 签名变体）。 */
    const PLAYURL_RE = /\/x\/player\/(wbi\/)?playurl(\?|$)/;

    type Obj = Record<string, unknown>;

    function streams(list: Obj[]): Obj[] {
      return (list || []).map((s) => ({
        id: s.id,
        codecs: s.codecs || '',
        bandwidth: s.bandwidth || 0,
        // 接口响应是 snake_case（base_url），页面 playinfo 是 camelCase。
        base: s.baseUrl || s.base_url || '',
      }));
    }

    /** 标题/分 P 元数据来自 __INITIAL_STATE__（接口响应不含这些字段）。 */
    function meta(): { p: number; pages: number; part: string; title: string; cid: number; bvid: string; aid: number } {
      const w = window as unknown as Obj;
      const st = w.__INITIAL_STATE__ as Obj | null;
      const videoData = st?.videoData as Obj | undefined;
      const pages = ((videoData?.pages as Obj[] | undefined) || []) as Obj[];
      const q = new URLSearchParams(location.search);
      const p = parseInt(q.get('p') || '1', 10) || 1;
      const cur = pages[p - 1] || {};
      let title = (videoData?.title as string) || '';
      if (!title) title = document.title.replace(/_哔哩哔哩.*$/, '');
      return {
        p,
        pages: pages.length || 1,
        part: (cur.part as string) || '',
        title,
        cid: (cur.cid as number) || (videoData?.cid as number) || 0,
        bvid: (videoData?.bvid as string) || '',
        aid: (videoData?.aid as number) || 0,
      };
    }

    /** __playinfo__ 来源（服务端渲染内嵌，可能只是空壳）。 */
    function extract(): Obj | null {
      const w = window as unknown as Obj;
      const pi = w.__playinfo__ as Obj | null;
      if (!pi) return null;
      const dash = pi.dash as Obj | undefined;
      const m = meta();
      return {
        ok: true,
        dash: !!dash,
        timelength: pi.timelength || 0,
        title: m.title,
        page: m.p,
        pages: m.pages,
        part: m.part,
        cid: m.cid,
        bvid: m.bvid,
        aid: m.aid,
        acceptIds: (pi.accept_quality as unknown[]) || [],
        acceptDesc: (pi.accept_description as unknown[]) || [],
        videos: dash ? streams((dash.video as Obj[]) || []) : [],
        audios: dash ? streams((dash.audio as Obj[]) || []) : [],
        durl: ((pi.durl as Obj[]) || []).map((d) => ({
          url: d.url || '',
          size: d.size || 0,
        })),
      };
    }

    /** playurl 接口响应（{code,data}）→ 统一播放信息；空壳/异常返回 null。 */
    function fromPlayurl(json: Obj | null, reqUrl: string): Obj | null {
      if (!json || json.code !== 0) return null;
      const d = json.data as Obj | undefined;
      if (!d || (!d.dash && (!d.durl || !(d.durl as Obj[]).length))) return null;
      const dash = d.dash as Obj | undefined;
      const q = new URLSearchParams(reqUrl.split('?')[1] || '');
      const m = meta();
      return {
        ok: true,
        dash: !!dash,
        timelength: d.timelength || 0,
        title: m.title,
        page: m.p,
        pages: m.pages,
        part: m.part,
        cid: parseInt(q.get('cid') || '', 10) || m.cid,
        bvid: q.get('bvid') || m.bvid,
        aid: parseInt(q.get('aid') || q.get('avid') || '', 10) || m.aid,
        acceptIds: (d.accept_quality as unknown[]) || [],
        acceptDesc: (d.accept_description as unknown[]) || [],
        videos: dash ? streams((dash.video as Obj[]) || []) : [],
        audios: dash ? streams((dash.audio as Obj[]) || []) : [],
        durl: ((d.durl as Obj[]) || []).map((x) => ({
          url: x.url || '',
          size: x.size || 0,
        })),
      };
    }

    /** 有可用流（DASH 分轨或旧版 durl）才算有效，空壳 playinfo 不推送。 */
    function usable(p: Obj): boolean {
      if (!p.ok) return false;
      if (p.dash) {
        return (p.videos as Obj[]).length > 0 || (p.audios as Obj[]).length > 0;
      }
      return (p.durl as Obj[]).length > 0;
    }

    let lastKey = '';

    function push(p: Obj | null): void {
      if (!p || !usable(p)) return;
      // key 含标题：接口响应先到（元数据尚未渲染）时，补全后能重推一次。
      const key = `${p.bvid}:${p.cid}:${p.timelength}:${p.title}`;
      if (key === lastKey) return; // 未变化不重复推送
      lastKey = key;
      window.postMessage({ source: SOURCE, type: 'bili', payload: p }, '*');
    }

    /** 接管 playurl 响应：fetch/XHR 均劫持，只读不改，异常全部吞掉。 */
    function onPlayurlResponse(reqUrl: string, data: unknown): void {
      try {
        const json =
          typeof data === 'string'
            ? (JSON.parse(data || 'null') as Obj | null)
            : (data as Obj | null);
        push(fromPlayurl(json, reqUrl));
      } catch {
        // 与播放信息无关的响应，忽略。
      }
    }

    const origFetch = window.fetch.bind(window);
    window.fetch = function (this: unknown, ...args: unknown[]): Promise<Response> {
      const input = args[0] as string | Request | URL | undefined;
      const url =
        typeof input === 'string' ? input : input instanceof URL ? input.href : input?.url || '';
      if (PLAYURL_RE.test(url)) {
        return origFetch(...(args as Parameters<typeof origFetch>)).then((res) => {
          try {
            void res.clone().text().then((t) => onPlayurlResponse(url, t));
          } catch {
            // 克隆失败不影响页面自身。
          }
          return res;
        });
      }
      return origFetch(...(args as Parameters<typeof origFetch>));
    };

    const origOpen = XMLHttpRequest.prototype.open;
    const origSend = XMLHttpRequest.prototype.send;
    // open/send 的重载签名复杂，用宽松实现 + 整体转型注册。
    const patchedOpen = function (this: XMLHttpRequest, ...args: unknown[]): void {
      try {
        const xhr = this as XMLHttpRequest & { knotUrl?: string };
        xhr.knotUrl = String(args[1] ?? '');
      } catch {}
      return (origOpen as (...a: unknown[]) => void).apply(this, args);
    };
    XMLHttpRequest.prototype.open = patchedOpen as typeof XMLHttpRequest.prototype.open;
    const patchedSend = function (this: XMLHttpRequest, ...args: unknown[]): void {
      try {
        const url = (this as XMLHttpRequest & { knotUrl?: string }).knotUrl || '';
        if (PLAYURL_RE.test(url)) {
          this.addEventListener('load', () => {
            try {
              const data =
                this.responseType === 'json'
                  ? this.response
                  : this.responseType === '' || this.responseType === 'text'
                    ? this.responseText
                    : null;
              onPlayurlResponse(url, data);
            } catch {}
          });
        }
      } catch {}
      return (origSend as (...a: unknown[]) => void).apply(this, args);
    };
    XMLHttpRequest.prototype.send = patchedSend as typeof XMLHttpRequest.prototype.send;

    // SPA 切视频后 __INITIAL_STATE__ 与 __playinfo__ 会短暂错位（新元数据 +
    // 旧流），轮询可能推出混合 payload；切视频必然伴随 playurl 请求（hook
    // 推正确数据），给 extract 推送 3s 冷却错开窗口期。
    let navAt = 0;

    function extractPush(): void {
      if (Date.now() - navAt < 3000) return;
      push(extract());
    }

    // playinfo 由服务端渲染，但部分页面播放器异步填充，轮询兜底（约 10 分钟）。
    let ticks = 0;
    const timer = setInterval(() => {
      extractPush();
      if (++ticks >= 240) clearInterval(timer);
    }, 2500);

    // B 站是 SPA：监听路由变化，切视频/分 P 后重新提取。
    for (const name of ['pushState', 'replaceState'] as const) {
      const orig = history[name].bind(history);
      history[name] = function (this: History, ...args: unknown[]) {
        const ret = (orig as (...a: unknown[]) => void)(...args);
        navAt = Date.now();
        return ret;
      };
    }
    window.addEventListener('popstate', () => {
      navAt = Date.now();
    });
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) extractPush();
    });
  },
});
