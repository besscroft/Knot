/**
 * 并行分片下载器。
 *
 * 国内到 Cloudflare 免费版的单连接大文件传输抖动严重(实测 1KB/s~1MB/s 波动,
 * 整包下载经常停滞),而多路 Range 分片实测可稳定在 MB/s 级(每条连接独立,
 * 速率可叠加);本站 Worker 对 Range 透传 GitHub 的 206 响应。
 *
 * 流程:HEAD 拿大小 → N 路并行 Range 拉分片 → 拼装 Blob → 触发保存。
 * 任何一步异常(HEAD 失败、对端不支持 Range、分片长度不符、重试耗尽)都
 * 返回 false,调用方回退为普通跳转下载(location.href,行为与旧行为一致)。
 */

/** 分片数:实测 4~8 条连接即可把抖动链路拉满,更多收益递减。 */
const PARTS = 6;
/** 拼装进内存的上限,超过直接走普通下载(更新包/安装包都在 MB 级)。 */
const MAX_BLOB_BYTES = 64 * 1024 * 1024;
/** 单个分片的重试次数(分片可独立重试,这是 Range 下载的天然优势)。 */
const PART_RETRIES = 2;

function filenameFromDisposition(disposition: string | null): string {
  const match = disposition?.match(/filename="?([^";]+)"?/i);
  return match?.[1] ?? "";
}

export interface RangedDownloadOptions {
  onProgress?: (percent: number) => void;
}

/**
 * 尝试并行分片下载 url 并触发浏览器保存。
 * 返回 true 表示已通过分片拼装触发保存;false 表示应回退普通下载。
 */
export async function rangedDownload(url: string, options: RangedDownloadOptions = {}): Promise<boolean> {
  const { onProgress } = options;

  let size = 0;
  let filename = "";
  try {
    const head = await fetch(url, { method: "HEAD" });
    if (!head.ok) return false;
    size = Number(head.headers.get("Content-Length") ?? 0);
    filename = filenameFromDisposition(head.headers.get("Content-Disposition"));
  } catch {
    return false;
  }
  if (!Number.isFinite(size) || size <= 0 || size > MAX_BLOB_BYTES) return false;

  const chunk = Math.ceil(size / PARTS);
  const parts: (ArrayBuffer | undefined)[] = new Array(PARTS);
  let done = 0;
  try {
    await Promise.all(
      Array.from({ length: PARTS }, (_, i) => {
        const start = i * chunk;
        const end = Math.min(start + chunk, size) - 1;
        return (async () => {
          let lastError: unknown = new Error("part not fetched");
          for (let attempt = 0; attempt <= PART_RETRIES; attempt++) {
            try {
              const res = await fetch(url, {
                headers: { Range: `bytes=${start}-${end}` },
                cache: "no-store",
              });
              if (!res.ok) throw new Error(`HTTP ${res.status}`);
              // 对端不支持 Range 会回 200 全量:退回普通下载,避免拼出错误文件
              if (res.status !== 206) throw new Error("range not supported");
              const buffer = await res.arrayBuffer();
              if (buffer.byteLength !== end - start + 1) throw new Error("length mismatch");
              parts[i] = buffer;
              done += 1;
              onProgress?.(Math.round((done / PARTS) * 100));
              return;
            } catch (error) {
              lastError = error;
            }
          }
          throw lastError;
        })();
      }),
    );
  } catch {
    return false;
  }

  const blob = new Blob(parts as BlobPart[], { type: "application/octet-stream" });
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(href), 60_000);
  return true;
}
