// B 站播放信息提取（MAIN world）：与后端 biliEvalCode（internal/service/
// bilibili.go）同款逻辑，避免两处口径漂移。实测（2026-11）当前 B 站视频页
// 已不再提供 window.__playinfo__，播放器改从 /x/player/wbi/playurl 接口拉
// 清单。但播放器何时发这个请求不受我们控制——自动播放被浏览器策略拦截、
// 播放器懒加载、标签页在后台打开（初始化被推迟）时可能晚到几十秒甚至不
// 发，被动劫持等不起。所以主来源是主动拉取：WBI 签名（nav 取 key + MD5）
// 直接请求 playurl，页面打开 / SPA 切视频 / 回前台即取，一两秒内出数据；
// 劫持播放器 playurl 响应保留为辅助来源（切画质/分 P 时播放器自己会重新
// 请求，口径与实际播放一致），__playinfo__ 轮询兜底（旧页面/内嵌场景）。
// matches 放宽到整个 bilibili.com：B 站是 SPA，从首页/搜索点进视频页是
// pushState 导航，不会重新注入内容脚本，只在 /video/* 注入会整站漏掉。
// MAIN world 没有扩展 API，提取结果经 window.postMessage 交给
// bilibili-bridge（ISOLATED）转发给背景页；弹窗「重新获取」指令也经同
// 一条桥反向传入。

export default defineContentScript({
  matches: ['*://*.bilibili.com/*'],
  world: 'MAIN',
  // document_start：钩子必须先于播放器启动，否则首个 playurl 请求会漏掉。
  runAt: 'document_start',
  main() {
    const SOURCE = 'knot-ext';
    const API_BASE = 'https://api.bilibili.com';

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

    // ---- 主动拉取（WBI 签名） ---------------------------------------------

    /** WBI 混淆表（bilibili-API-collect 公开算法，64 元素下标置换）。 */
    const MIXIN_TAB = [
      46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49,
      33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61,
      26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36,
      20, 34, 44, 52,
    ];

    /** 紧凑 MD5（WBI 签名用）；UTF-8 输入，小写十六进制输出。 */
    function md5(input: string): string {
      const bytes = Array.from(new TextEncoder().encode(input));
      const bitLen = bytes.length * 8;
      bytes.push(0x80);
      while (bytes.length % 64 !== 56) bytes.push(0);
      for (let i = 0; i < 8; i++) bytes.push(Math.floor(bitLen / 2 ** (8 * i)) & 0xff);

      const S = [
        7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
        5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20,
        4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
        6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21,
      ];
      const K: number[] = [];
      for (let i = 0; i < 64; i++) K.push(Math.floor(Math.abs(Math.sin(i + 1)) * 4294967296));

      let a0 = 0x67452301;
      let b0 = 0xefcdab89;
      let c0 = 0x98badcfe;
      let d0 = 0x10325476;

      for (let off = 0; off < bytes.length; off += 64) {
        const M: number[] = [];
        for (let i = 0; i < 16; i++) {
          M[i] =
            bytes[off + i * 4]! |
            (bytes[off + i * 4 + 1]! << 8) |
            (bytes[off + i * 4 + 2]! << 16) |
            (bytes[off + i * 4 + 3]! << 24);
        }
        let A = a0;
        let B = b0;
        let C = c0;
        let D = d0;
        for (let i = 0; i < 64; i++) {
          let F: number;
          let g: number;
          if (i < 16) {
            F = (B & C) | (~B & D);
            g = i;
          } else if (i < 32) {
            F = (D & B) | (~D & C);
            g = (5 * i + 1) % 16;
          } else if (i < 48) {
            F = B ^ C ^ D;
            g = (3 * i + 5) % 16;
          } else {
            F = C ^ (B | ~D);
            g = (7 * i) % 16;
          }
          F = (F + A + K[i]! + M[g]!) | 0;
          A = D;
          D = C;
          C = B;
          B = (B + ((F << S[i]!) | (F >>> (32 - S[i]!)))) | 0;
        }
        a0 = (a0 + A) | 0;
        b0 = (b0 + B) | 0;
        c0 = (c0 + C) | 0;
        d0 = (d0 + D) | 0;
      }

      const hex = (n: number): string => {
        let out = '';
        for (let i = 0; i < 4; i++) out += ((n >>> (8 * i)) & 0xff).toString(16).padStart(2, '0');
        return out;
      };
      return hex(a0) + hex(b0) + hex(c0) + hex(d0);
    }

    /** WBI key 每天轮换，页面生命周期内缓存 1 小时足够。 */
    let wbiCache: { key: string; at: number } | null = null;

    /** nav 接口拿 img/sub key → 混淆表重排取前 32 位。匿名也能拿到。 */
    async function wbiMixinKey(): Promise<string> {
      if (wbiCache && Date.now() - wbiCache.at < 3600000) return wbiCache.key;
      const res = (await origFetch(`${API_BASE}/x/web-interface/nav`, {
        credentials: 'include',
      }).then((r) => r.json())) as Obj | null;
      const img = ((res?.data as Obj | undefined)?.wbi_img as Obj | undefined) || {};
      const keyOf = (u: unknown): string => String(u || '').split('/').pop()?.split('.')[0] || '';
      const raw = keyOf(img.img_url) + keyOf(img.sub_url);
      if (raw.length !== 64) throw new Error('wbi key 不可用');
      wbiCache = {
        key: MIXIN_TAB.map((i) => raw[i] ?? '').join('').slice(0, 32),
        at: Date.now(),
      };
      return wbiCache.key;
    }

    /** 当前地址是否视频页；支持 BV 号与旧版 av 号。 */
    function currentVideo(): { bvid: string; aid: number } | null {
      const seg = location.pathname.match(/^\/video\/([^/]+)/)?.[1];
      if (!seg) return null;
      if (/^BV[0-9A-Za-z]+$/.test(seg)) return { bvid: seg, aid: 0 };
      const av = seg.match(/^av(\d+)$/i);
      if (av) return { bvid: '', aid: Number(av[1]) };
      return null;
    }

    /** view 接口拿标题/分 P/cid：SPA 刚切视频时比 __INITIAL_STATE__ 新。 */
    async function fetchView(bvid: string, aid: number): Promise<Obj | null> {
      try {
        const q = bvid ? `bvid=${encodeURIComponent(bvid)}` : `aid=${aid}`;
        const res = (await origFetch(`${API_BASE}/x/web-interface/view?${q}`, {
          credentials: 'include',
        }).then((r) => r.json())) as Obj | null;
        if (!res || res.code !== 0) return null;
        return (res.data as Obj | undefined) || null;
      } catch {
        return null;
      }
    }

    // 主动拉取的去重/放弃状态：目标 = bvid|p。成功后不再重复；连续失败
    // 3 次放弃（充电专属等永久失败场景），等弹窗「重新获取」手动触发。
    let activeDoneVid = '';
    let activeFailTarget = '';
    let activeFails = 0;
    let activeInflight: Promise<void> | null = null;

    async function runActive(vid: { bvid: string; aid: number }, p: number, target: string): Promise<void> {
      activeFailTarget = target;
      try {
        const d = await fetchView(vid.bvid, vid.aid);
        if (!d) throw new Error('view 接口失败');
        const pages = ((d.pages as Obj[] | undefined) || []) as Obj[];
        const cur = pages[p - 1] || {};
        const bvid = (d.bvid as string) || vid.bvid;
        const aid = (d.aid as number) || vid.aid;
        const cid = (cur.cid as number) || (d.cid as number) || 0;
        if (!cid) throw new Error('cid 缺失');

        const mixin = await wbiMixinKey();
        const params: Obj = {
          bvid,
          aid: String(aid || ''),
          cid: String(cid),
          qn: '0',
          fnval: '4048',
          fnver: '0',
          fourk: '1',
          platform: 'web',
          high_quality: '1',
          try_look: '1',
        };
        if (!params.aid) delete params.aid;
        const wts = Math.floor(Date.now() / 1000);
        const signed: Obj = { ...params, wts: String(wts) };
        // 签名的查询串与实际发送的完全一致（排序 + 同样的转义），杜绝两处拼法漂移。
        const query = Object.keys(signed)
          .sort()
          .map((k) => `${k}=${encodeURIComponent(String(signed[k]).replace(/[!'()*]/g, ''))}`)
          .join('&');
        const url = `${API_BASE}/x/player/wbi/playurl?${query}&w_rid=${md5(query + mixin)}&wts=${wts}`;
        const res = (await origFetch(url, { credentials: 'include' }).then((r) => r.json())) as Obj | null;
        const payload = fromPlayurl(res, url);
        if (!payload) throw new Error('playurl 无可用清单');

        // 元数据以 view 接口为准（SPA 切视频后页面状态可能还是旧的）。
        payload.title = (d.title as string) || '';
        payload.page = p;
        payload.pages = pages.length || 1;
        payload.part = (cur.part as string) || '';
        payload.cid = cid;
        payload.bvid = bvid;
        payload.aid = aid;
        activeDoneVid = target;
        activeFails = 0;
        push(payload);
      } catch {
        activeFails++;
      }
    }

    /** 当前视频还没有数据时主动拉一次（定时器/事件兜底入口）。 */
    function ensureActive(): void {
      const vid = currentVideo();
      if (!vid || document.hidden || activeInflight) return;
      const p = parseInt(new URLSearchParams(location.search).get('p') || '1', 10) || 1;
      const target = `${vid.bvid || `av${vid.aid}`}|p${p}`;
      if (target === activeDoneVid) return;
      if (target !== activeFailTarget) activeFails = 0;
      if (activeFails >= 3) return;
      activeInflight = runActive(vid, p, target).finally(() => {
        activeInflight = null;
      });
    }

    /** 弹窗「重新获取」：清掉完成/失败状态强拉一次（忽略后台标签页状态）。 */
    function forceActive(): void {
      if (activeInflight) return; // 正在跑就等它，避免并发双请求
      const vid = currentVideo();
      if (!vid) return;
      activeFails = 0;
      activeDoneVid = '';
      const p = parseInt(new URLSearchParams(location.search).get('p') || '1', 10) || 1;
      const target = `${vid.bvid || `av${vid.aid}`}|p${p}`;
      activeInflight = runActive(vid, p, target).finally(() => {
        activeInflight = null;
      });
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
    // 与主动拉取都推正确数据），给 extract 推送 3s 冷却错开窗口期。
    let navAt = 0;

    function extractPush(): void {
      if (Date.now() - navAt < 3000) return;
      push(extract());
    }

    // 弹窗「重新获取」经 bridge 转发的反向指令。
    window.addEventListener('message', (event: MessageEvent) => {
      if (event.source !== window) return;
      const d = event.data as { source?: string; type?: string } | null;
      if (!d || d.source !== SOURCE || d.type !== 'refresh') return;
      forceActive();
    });

    // B 站是 SPA：路由变化后重新提取 + 主动拉取（新视频立即取清单）。
    for (const name of ['pushState', 'replaceState'] as const) {
      const orig = history[name].bind(history);
      history[name] = function (this: History, ...args: unknown[]) {
        const ret = (orig as (...a: unknown[]) => void)(...args);
        navAt = Date.now();
        ensureActive();
        return ret;
      };
    }
    window.addEventListener('popstate', () => {
      navAt = Date.now();
      ensureActive();
    });

    // 后台标签页会推迟播放器初始化，回前台时若还没有数据立即补一次。
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) {
        extractPush();
        ensureActive();
      }
    });

    // 页面就绪即主动拉取，不依赖播放器何时请求。
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', () => ensureActive());
    } else {
      ensureActive();
    }

    // playinfo 由服务端渲染的旧页面兜底轮询（约 10 分钟）；ensureActive
    // 内部自带去重与放弃逻辑，空转成本可忽略。
    let ticks = 0;
    const timer = setInterval(() => {
      extractPush();
      ensureActive();
      if (++ticks >= 240) clearInterval(timer);
    }, 2500);
  },
});
