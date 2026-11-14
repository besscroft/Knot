/**
 * Knot 官网下载代理:
 * 解析 GitHub 最新 Release 的资产列表,把安装包流式代理回给用户,
 * 避免国内用户直连 GitHub Releases 下载超时。
 *
 * 路由:
 *   /download/latest/windows          knot-setup-<ver>-amd64.exe
 *   /download/latest/windows-arm64    knot-setup-<ver>-arm64.exe
 *   /download/latest/windows-portable knot-<ver>-windows-amd64-portable.zip
 *   /download/latest/macos            *.dmg(文件名不固定)
 *   /download/latest/macos-zip        knot-<ver>-macos-universal.zip
 *   /download/latest/info             最新版本信息 JSON(供页面展示版本号)
 *
 * 解析失败或找不到目标资产时,降级为 302 跳转 GitHub Releases 页面。
 * 其余请求交给 Workers Assets(静态文件 + SPA fallback)。
 */

const REPO = "besscroft/Knot";
const RELEASES_PAGE = `https://github.com/${REPO}/releases/latest`;
const RELEASE_API = `https://api.github.com/repos/${REPO}/releases/latest`;

// GitHub API 未认证限额为 60 次/小时/IP,两级缓存 10 分钟足够规避
const CACHE_TTL_MS = 10 * 60 * 1000;
// Cache API 要求绝对 URL 作为 key,用固定合成地址
const CACHE_KEY = "https://cache.knot-website.internal/github-release.json";

interface AssetsBinding {
  fetch: (request: Request) => Promise<Response>;
}

interface Env {
  ASSETS: AssetsBinding;
}

interface ReleaseAsset {
  name: string;
  size: number;
  browser_download_url: string;
}

interface ReleaseInfo {
  tag_name: string;
  published_at: string;
  assets: ReleaseAsset[];
}

interface SimpleCache {
  match: (request: RequestInfo | URL) => Promise<Response | undefined>;
  put: (request: RequestInfo | URL, response: Response) => Promise<void>;
}

declare const caches: { default: SimpleCache };

let memCache: { info: ReleaseInfo; expiresAt: number } | undefined;

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

  try {
    const res = await fetch(RELEASE_API, {
      headers: {
        "User-Agent": "knot-website-download-proxy",
        Accept: "application/vnd.github+json",
      },
    });
    if (!res.ok) {
      return null;
    }
    const info = (await res.json()) as ReleaseInfo;
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
  } catch {
    return null;
  }
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
    return fallbackToReleasesPage();
  }
  if (!upstream.ok && upstream.status !== 206) {
    return fallbackToReleasesPage();
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
  headersOut.set("Cache-Control", "public, max-age=300");

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
    // 静态资产与 SPA fallback 交给 Workers Assets
    return env.ASSETS.fetch(request);
  },
};
