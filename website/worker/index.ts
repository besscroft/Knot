/**
 * Knot 官网下载代理:
 * 解析 GitHub 最新 Release 的资产列表,把安装包流式代理回给用户,
 * 避免国内用户直连 GitHub Releases 下载超时。
 *
 * 解析顺序:
 *   1. 抓 github.com/releases/latest 的 302 拿 tag,再抓 expanded_assets
 *      片段解析资产名 —— GitHub 页面没有 API 那种按 IP 的限额,
 *      Cloudflare Workers 的共享出口 IP 不会被打爆(主路径);
 *   2. api.github.com 的 releases/latest(兜底,未认证 60 次/小时/IP,
 *      在 Cloudflare 共享出口 IP 上经常 403)。
 *
 * 路由:
 *   /download/latest/windows          knot-setup-<ver>-amd64.exe
 *   /download/latest/windows-arm64    knot-setup-<ver>-arm64.exe
 *   /download/latest/windows-portable knot-<ver>-windows-amd64-portable.zip
 *   /download/latest/macos            *.dmg(文件名不固定)
 *   /download/latest/macos-zip        knot-<ver>-macos-universal.zip
 *   /download/latest/info             最新版本信息 JSON(供页面展示版本号)
 *   /update/<文件名>                  按精确文件名代理 release 资产,给 mygo
 *                                     更新器用(manifest/tar.gz/delta,url 均
 *                                     编译期为本域,见 Knot 的 mygo.config.ts)
 *
 * /download/* 解析失败或找不到目标资产时,降级为 302 跳转 GitHub Releases
 * 页面;/update/* 找不到则 404(更新器语义)。其余请求交给 Workers Assets
 * (静态文件 + SPA fallback)。
 */

const REPO = "besscroft/Knot";
const RELEASES_PAGE = `https://github.com/${REPO}/releases/latest`;
const RELEASE_API = `https://api.github.com/repos/${REPO}/releases/latest`;

const GH_HEADERS = { "User-Agent": "knot-website-download-proxy" };

interface AssetsBinding {
  fetch: (request: Request) => Promise<Response>;
}

interface Env {
  ASSETS: AssetsBinding;
}

// 解析结果两级缓存 10 分钟,减少上游请求
const CACHE_TTL_MS = 10 * 60 * 1000;
// Cache API 要求绝对 URL 作为 key,用固定合成地址
const CACHE_KEY = "https://cache.knot-website.internal/github-release.json";

interface ReleaseAsset {
  name: string;
  size: number;
  browser_download_url: string;
}

interface ReleaseInfo {
  tag_name: string;
  published_at: string;
  source: string;
  assets: ReleaseAsset[];
}

interface SimpleCache {
  match: (request: RequestInfo | URL) => Promise<Response | undefined>;
  put: (request: RequestInfo | URL, response: Response) => Promise<void>;
}

declare const caches: { default: SimpleCache };

let memCache: { info: ReleaseInfo; expiresAt: number } | undefined;

/** 从 releases/latest 的 302 Location 里取 tag,要求 tag 只含安全字符(要拼进 URL)。 */
function tagFromLocation(location: string): string | null {
  const tag = decodeURIComponent(location.split("/").filter(Boolean).pop() ?? "");
  return /^[\w.+-]+$/.test(tag) ? tag : null;
}

/** 主路径:github.com 页面抓取,无 API 限额。 */
async function resolveViaHtml(): Promise<ReleaseInfo | null> {
  try {
    const res = await fetch(RELEASES_PAGE, { redirect: "manual", headers: GH_HEADERS });
    const location = res.headers.get("Location");
    if ((res.status !== 301 && res.status !== 302) || !location) {
      console.warn(`[download] html 解析失败:latest 未重定向 status=${res.status}`);
      return null;
    }
    const tag = tagFromLocation(location);
    if (!tag) {
      console.warn(`[download] html 解析失败:Location 里取不到 tag location=${location}`);
      return null;
    }

    const frag = await fetch(
      `https://github.com/${REPO}/releases/expanded_assets/${encodeURIComponent(tag)}`,
      { headers: GH_HEADERS },
    );
    if (!frag.ok) {
      console.warn(`[download] html 解析失败:expanded_assets status=${frag.status}`);
      return null;
    }
    const html = await frag.text();
    const names = new Set<string>();
    for (const match of html.matchAll(/releases\/download\/[^"'?\s]+/g)) {
      const name = decodeURIComponent(match[0].split("/").pop() ?? "");
      if (name) {
        names.add(name);
      }
    }
    if (names.size === 0) {
      console.warn(`[download] html 解析失败:expanded_assets 里没有资产 tag=${tag}`);
      return null;
    }
    return {
      tag_name: tag,
      published_at: "",
      source: "html",
      assets: [...names].map((name) => ({
        name,
        size: 0,
        browser_download_url: `https://github.com/${REPO}/releases/download/${tag}/${name}`,
      })),
    };
  } catch (err) {
    console.warn("[download] html 解析异常:", err);
    return null;
  }
}

/** 兜底:GitHub API,结构化但限额严格。 */
async function resolveViaApi(): Promise<ReleaseInfo | null> {
  try {
    const res = await fetch(RELEASE_API, {
      headers: { ...GH_HEADERS, Accept: "application/vnd.github+json" },
    });
    if (!res.ok) {
      console.warn(`[download] api 解析失败:status=${res.status}(403 多为共享出口 IP 限额)`);
      return null;
    }
    const info = (await res.json()) as Omit<ReleaseInfo, "source">;
    return { ...info, source: "api" };
  } catch (err) {
    console.warn("[download] api 解析异常:", err);
    return null;
  }
}

async function resolveRelease(): Promise<ReleaseInfo | null> {
  if (memCache && memCache.expiresAt > Date.now()) {
    return memCache.info;
  }

  try {
    const cached = await caches.default.match(CACHE_KEY);
    if (cached) {
      const info = (await cached.json()) as ReleaseInfo;
      memCache = { info, expiresAt: Date.now() + CACHE_TTL_MS };
      return info;
    }
  } catch {
    // 缓存读取失败就继续走网络
  }

  for (const resolve of [resolveViaHtml, resolveViaApi]) {
    const info = await resolve();
    if (info) {
      memCache = { info, expiresAt: Date.now() + CACHE_TTL_MS };
      try {
        await caches.default.put(
          CACHE_KEY,
          new Response(JSON.stringify(info), {
            headers: {
              "Content-Type": "application/json",
              "Cache-Control": `public, max-age=${CACHE_TTL_MS / 1000}`,
            },
          }),
        );
      } catch {
        // 缓存写入失败不影响本次响应
      }
      return info;
    }
  }
  return null;
}

const ASSET_ROUTES: Record<string, (assets: ReleaseAsset[]) => ReleaseAsset | undefined> = {
  "/download/latest/windows": (assets) =>
    assets.find((x) => /^knot-setup-[\d.]+-amd64\.exe$/.test(x.name)),
  "/download/latest/windows-arm64": (assets) =>
    assets.find((x) => /^knot-setup-[\d.]+-arm64\.exe$/.test(x.name)),
  "/download/latest/windows-portable": (assets) =>
    assets.find((x) => /^knot-[\d.]+-windows-amd64-portable\.zip$/.test(x.name)),
  "/download/latest/macos": (assets) => assets.find((x) => x.name.endsWith(".dmg")),
  "/download/latest/macos-zip": (assets) =>
    assets.find((x) => /^knot-[\d.]+-macos-universal\.zip$/.test(x.name)),
};

function fallbackToReleasesPage(): Response {
  return Response.redirect(RELEASES_PAGE, 302);
}

async function handleDownload(request: Request, pick: (assets: ReleaseAsset[]) => ReleaseAsset | undefined): Promise<Response> {
  const release = await resolveRelease();
  if (!release) {
    return fallbackToReleasesPage();
  }
  const asset = pick(release.assets);
  if (!asset) {
    return fallbackToReleasesPage();
  }
  return proxyAsset(request, asset, 300, fallbackToReleasesPage);
}

function updateFileNotFound(): Response {
  return new Response(JSON.stringify({ error: "update file not found" }), {
    status: 404,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
  });
}

/** 更新器取件:/update/<文件名>,按精确名在最新 release 里找;404 语义,不做页面降级。 */
async function handleUpdateFile(request: Request, filename: string): Promise<Response> {
  if (!/^[\w.-]+$/.test(filename)) {
    return updateFileNotFound();
  }
  const release = await resolveRelease();
  const asset = release?.assets.find((x) => x.name === filename);
  if (!asset) {
    return updateFileNotFound();
  }
  // manifest 变更要及时被检查到,缓存从短
  const isManifest = filename.startsWith("update-") && filename.endsWith(".json");
  return proxyAsset(request, asset, isManifest ? 60 : 300, updateFileNotFound);
}

/** 把 release 资产流式代理回客户端;onFail 是上游失败时的降级响应。 */
async function proxyAsset(request: Request, asset: ReleaseAsset, cacheTtl: number, onFail: () => Response): Promise<Response> {
  // 透传 Range,让下载工具可以断点续传
  const headers: Record<string, string> = { "User-Agent": "knot-website-download-proxy" };
  const range = request.headers.get("Range");
  if (range) {
    headers.Range = range;
  }

  let upstream: Response;
  try {
    upstream = await fetch(asset.browser_download_url, {
      method: request.method === "HEAD" ? "HEAD" : "GET",
      headers,
    });
  } catch {
    return onFail();
  }
  if (!upstream.ok && upstream.status !== 206) {
    return onFail();
  }

  const headersOut = new Headers();
  headersOut.set("Content-Type", "application/octet-stream");
  headersOut.set("Content-Disposition", `attachment; filename="${asset.name}"`);
  const length = upstream.headers.get("Content-Length");
  if (length) {
    headersOut.set("Content-Length", length);
  }
  const contentRange = upstream.headers.get("Content-Range");
  if (contentRange) {
    headersOut.set("Content-Range", contentRange);
  }
  headersOut.set("Accept-Ranges", upstream.headers.get("Accept-Ranges") ?? "bytes");
  headersOut.set("Cache-Control", `public, max-age=${cacheTtl}`);

  return new Response(request.method === "HEAD" ? null : upstream.body, {
    status: upstream.status,
    headers: headersOut,
  });
}

async function handleInfo(): Promise<Response> {
  const release = await resolveRelease();
  if (!release) {
    return new Response(JSON.stringify({ error: "release unavailable" }), {
      status: 503,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  }
  const body = {
    version: release.tag_name.replace(/^v/, ""),
    tag: release.tag_name,
    publishedAt: release.published_at,
    source: release.source,
    assets: release.assets.map((asset) => ({ name: asset.name, size: asset.size })),
  };
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json", "Cache-Control": "public, max-age=300" },
  });
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const { pathname } = new URL(request.url);
    if (pathname === "/download/latest/info") {
      return handleInfo();
    }
    const pick = ASSET_ROUTES[pathname];
    if (pick) {
      return handleDownload(request, pick);
    }
    if (pathname.startsWith("/update/")) {
      return handleUpdateFile(request, pathname.slice("/update/".length));
    }
    // 静态资产与 SPA fallback 交给 Workers Assets
    return env.ASSETS.fetch(request);
  },
};
