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
 * mediaFileUrl / mediaThumbUrl 构造媒体服务地址。
 * WebView2 加载不了自定义 scheme：mygo 把 media://localhost/… 映射为
 * http://media.localhost/… 服务（schemehost），必须用 http 形式才能到达
 * Go 侧的 mediaHandler（media:// 形式的请求会被 WebView2 原样放行并失败）。
 */
export function mediaFileUrl(taskID: string): string {
  return `http://media.localhost/file/${taskID}`;
}

export function mediaThumbUrl(taskID: string): string {
  return `http://media.localhost/thumb/${taskID}`;
}
