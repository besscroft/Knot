import type { BiliPayload, CreateResult, ExtSettings, YouTubeQuality } from './types';

export const DEFAULT_SERVER = 'http://127.0.0.1:22333';

const SETTINGS_KEY = 'settings';

export async function loadSettings(): Promise<ExtSettings> {
  const stored = (await browser.storage.local.get(SETTINGS_KEY))[SETTINGS_KEY] as
    | ExtSettings
    | undefined;
  return { serverBase: stored?.serverBase || DEFAULT_SERVER };
}

export async function saveSettings(settings: ExtSettings): Promise<void> {
  await browser.storage.local.set({ [SETTINGS_KEY]: settings });
}

/** 规整用户输入的服务器地址：补协议头、去尾部斜杠。 */
export function normalizeBase(input: string): string {
  let base = input.trim().replace(/\/+$/, '');
  if (base && !/^https?:\/\//i.test(base)) base = 'http://' + base;
  return base;
}

/** 连通检测；失败抛出可直接展示的中文错误。 */
export async function ping(serverBase: string): Promise<void> {
  let res: Response;
  try {
    res = await fetch(`${serverBase}/api/ping`, { cache: 'no-store' });
  } catch {
    throw new Error('无法连接 Knot，请确认应用已运行且地址正确');
  }
  if (!res.ok) throw new Error(`Knot 返回异常（HTTP ${res.status}）`);
}

/** /api/create 的请求体（对齐后端 extCreateRequest）。 */
export interface CreateBody {
  url?: string;
  name?: string;
  format?: string;
  referer?: string;
  cookie?: string;
  userAgent?: string;
  bili?: BiliPayload;
  qualityId?: number;
}

/** 创建下载任务；失败抛出可直接展示的中文错误。 */
export async function createOnKnot(
  serverBase: string,
  body: CreateBody,
): Promise<CreateResult> {
  let res: Response;
  try {
    res = await fetch(`${serverBase}/api/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Knot-Client': 'browser-extension' },
      body: JSON.stringify(body),
    });
  } catch {
    throw new Error('无法连接 Knot，请确认应用已运行且地址正确');
  }
  const data = (await res.json().catch(() => null)) as CreateResult | null;
  if (!res.ok || !data?.ok) {
    throw new Error(data?.error || `Knot 返回异常（HTTP ${res.status}）`);
  }
  return data;
}

/** /api/youtube/resolve 的响应（对齐后端 YouTubeInfo）。 */
export interface YouTubeInfo {
  title: string;
  /** 秒，0 = 未知。 */
  duration: number;
  qualities: YouTubeQuality[];
}

/** 解析 YouTube 视频信息（画质清单）；失败抛出可直接展示的中文错误。
 * 解析在 Knot 侧跑 yt-dlp，首次使用会先自动获取二进制，耗时可能较长。 */
export async function resolveYouTube(serverBase: string, url: string): Promise<YouTubeInfo> {
  let res: Response;
  try {
    res = await fetch(`${serverBase}/api/youtube/resolve`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Knot-Client': 'browser-extension' },
      body: JSON.stringify({ url }),
    });
  } catch {
    throw new Error('无法连接 Knot，请确认应用已运行且地址正确');
  }
  const data = (await res.json().catch(() => null)) as
    | { ok: boolean; error?: string; info?: YouTubeInfo }
    | null;
  if (!res.ok || !data?.ok || !data.info) {
    throw new Error(data?.error || `Knot 返回异常（HTTP ${res.status}）`);
  }
  return data.info;
}
