// 与 Knot 后端（internal/service）对齐的类型与消息协议。

/** 嗅探资源类型，口径与 Knot 应用内嗅探一致（browser.go preload）。 */
export type SniffType =
  | 'hls'
  | 'video'
  | 'audio'
  | 'image'
  | 'archive'
  | 'document';

export interface SniffItem {
  url: string;
  name: string;
  type: SniffType;
  /** Content-Length 字节数，未知为 0。 */
  size: number;
  /** 捕获时间戳（毫秒），列表排序用。 */
  time: number;
}

/** 对齐后端 biliStreamPayload（internal/service/bilibili.go）。 */
export interface BiliStream {
  id: number;
  codecs: string;
  /** bit/s */
  bandwidth: number;
  base: string;
}

export interface BiliDurlItem {
  url: string;
  size: number;
}

/** 对齐后端 biliPayload：MAIN-world 内容脚本从 __playinfo__ 提取的原始形状。 */
export interface BiliPayload {
  ok: boolean;
  dash: boolean;
  /** 视频时长（毫秒），估算大小用。 */
  timelength: number;
  title: string;
  page: number;
  pages: number;
  part: string;
  cid: number;
  bvid: string;
  aid: number;
  acceptIds: number[];
  acceptDesc: string[];
  videos: BiliStream[];
  audios: BiliStream[];
  durl: BiliDurlItem[];
}

export interface BiliQuality {
  id: number;
  desc: string;
  codecs: string;
  /** 估算字节数，0 = 未知。 */
  size: number;
}

/** 对齐后端 YouTubeQuality（internal/service/youtube.go）。 */
export interface YouTubeQuality {
  /** 档位值（短边像素），0 = 最佳。 */
  id: number;
  desc: string;
  codecs?: string;
  /** 估算字节数，0 = 未知。 */
  size: number;
}

/** YouTube 视频解析状态；url 对不上当前视频页即视为过期。 */
export interface YouTubeState {
  url: string;
  ok: boolean;
  title: string;
  /** 秒，0 = 未知。 */
  duration: number;
  qualities: YouTubeQuality[];
  /** ok=false 时的失败原因。 */
  error: string;
}

/** background 按标签页维护的嗅探状态（chrome.storage.session）。 */
export interface TabData {
  /** 顶层页面地址，作为发送给 Knot 的 Referer。 */
  pageUrl: string;
  sniffs: SniffItem[];
  /** B 站播放信息（内容脚本上报），无则为空。 */
  bili: BiliPayload | null;
  /** YouTube 解析状态（弹窗打开时按需解析），无则为空。 */
  youtube: YouTubeState | null;
}

export interface ExtSettings {
  /** Knot 服务器地址，如 http://127.0.0.1:22333。 */
  serverBase: string;
}

/** 后端 /api/create 的响应（extapi.go writeExtTask/writeExtErr）。 */
export interface CreateResult {
  ok: boolean;
  error?: string;
  task?: {
    id: string;
    name: string;
    status: string;
  };
}

/** 单条资源的发送结果（popup 逐项展示）。 */
export interface SendResult {
  url: string;
  ok: boolean;
  error?: string;
}

/** popup / 内容脚本 → background 的消息。 */
export type BgRequest =
  | { type: 'tabData'; tabId: number }
  | { type: 'popupOpened'; tabId: number }
  | { type: 'send'; tabId: number; items: SendItem[] }
  | { type: 'sendBili'; tabId: number; qualityId: number; format: string; name: string }
  | { type: 'biliPayload'; payload: BiliPayload }
  | { type: 'biliRefresh'; tabId: number }
  | { type: 'resolveYouTube'; tabId: number; url: string; force?: boolean }
  | { type: 'sendYouTube'; url: string; qualityId: number; format: string; name: string }
  | { type: 'ping' };

/** 确认后的待发送项；name 为空 = 由 Knot 从链接自动推断。 */
export interface SendItem {
  url: string;
  name?: string;
}
