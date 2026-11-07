/** formatBytes 把字节数格式化为人类可读的大小（未知传负数）。 */
export function formatBytes(n: number): string {
  if (n < 0) return "未知大小";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const digits = i === 0 ? 0 : v >= 100 ? 1 : 2;
  return `${v.toFixed(digits)} ${units[i]}`;
}

/** formatSpeed 把字节/秒格式化为 "3.4 MB/s"。 */
export function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return "";
  return `${formatBytes(bytesPerSec)}/s`;
}

/** formatProgress 返回 "12.3 MB / 45.6 MB" 形式的进度文案。 */
export function formatProgress(downloaded: number, total: number): string {
  if (total < 0) return formatBytes(downloaded);
  return `${formatBytes(downloaded)} / ${formatBytes(total)}`;
}

/** formatDuration 把秒格式化为 "3:12" / "1:03:45"（未知传非正数）。 */
export function formatDuration(sec: number): string {
  if (!(sec > 0)) return "";
  const s = Math.round(sec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  const mm = h > 0 ? String(m).padStart(2, "0") : String(m);
  const ss = String(r).padStart(2, "0");
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

/** mediaKindOf 按文件扩展名分类：image / video / audio / other。 */
export function mediaKindOf(name: string): "image" | "video" | "audio" | "other" {
  const ext = name.split("#")[0].split("?")[0].split(".").pop()?.toLowerCase() ?? "";
  if (["jpg", "jpeg", "png", "gif", "webp", "bmp", "avif"].includes(ext)) return "image";
  if (["mp4", "mkv", "mov", "webm", "m4v", "avi", "flv", "ts", "mpg", "mpeg", "wmv"].includes(ext)) return "video";
  if (["mp3", "m4a", "aac", "flac", "wav", "ogg", "opus", "wma"].includes(ext)) return "audio";
  return "other";
}

/**
 * 媒体服务地址。Go 侧起 loopback HTTP 服务器（127.0.0.1 随机端口 +
 * 随机 token，见 internal/service/media.go），base 由 App.mediaBase()
 * 在启动时拉取——不能用 mygo 自定义协议：Windows/WebView2 后端会把
 * 自定义协议响应整体缓冲进内存，大视频一点播放就 OOM 闪退。
 */
let mediaBase = "";

/** setMediaBase 注入启动时从 Go 侧取到的媒体服务 base URL。 */
export function setMediaBase(base: string) {
  mediaBase = base.replace(/\/+$/, "");
}

export function mediaFileUrl(taskID: string): string {
  return mediaBase ? `${mediaBase}/file/${taskID}` : "";
}

export function mediaThumbUrl(taskID: string): string {
  return mediaBase ? `${mediaBase}/thumb/${taskID}` : "";
}
