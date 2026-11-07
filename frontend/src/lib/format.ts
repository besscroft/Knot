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
