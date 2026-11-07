import { useEffect, useRef, useState } from "react";
import { Browser, events, type BilibiliInfo, type DownloadRequest, type Sniffed } from "../mygo";
import { IconDownload, IconRefresh, IconSearch, IconX } from "../components/icons";
import { Button, IconButton, Input, Modal } from "../components/ui";
import { formatBytes } from "../lib/format";
import { cn } from "../lib/utils";

const TYPE_LABEL: Record<string, string> = {
  info: "提示",
  bili: "B站视频",
  hls: "HLS",
  video: "视频",
  audio: "音频",
  image: "图片",
  archive: "压缩包",
  document: "文档",
  link: "链接",
  other: "其他",
};

const TYPE_COLOR: Record<string, string> = {
  info: "text-muted bg-panel-2",
  bili: "text-[#fb7299] bg-[#fb7299]/10",
  hls: "text-danger bg-danger/10",
  video: "text-violet-500 bg-violet-500/10",
  audio: "text-cyan-500 bg-cyan-500/10",
  image: "text-accent bg-accent-soft",
  archive: "text-orange-500 bg-orange-500/10",
  document: "text-success bg-success/10",
  link: "text-muted bg-panel-2",
  other: "text-muted bg-panel-2",
};

/**
 * 右上角嗅探浮窗：展示浏览页面里可下载的资源。
 * 导航控件在主窗口的常驻工具栏里，这里只管嗅探与下载；
 * 收起后可从工具栏的「嗅探」按钮重新打开。
 * 下载前在此浮窗内弹出文件名确认框（浏览时主窗口内容区被站点窗口
 * 覆盖，只有浮窗始终可见）；网页触发的下载经 download-request 事件
 * 进入同一队列。
 */
export function SniffPanel() {
  const [sniffed, setSniffed] = useState<Sniffed[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [requests, setRequests] = useState<DownloadRequest[]>([]);
  const [scanning, setScanning] = useState(false);
  const [queued, setQueued] = useState(false);
  const [biliOpen, setBiliOpen] = useState(false);
  const scanTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    void Browser.scan(); // 浮窗打开时补一次嗅探
    const offState = events.browserState.on((s) => {
      if (s.mode === "app") {
        // 只有真正退出（会话终结）才清空；暂停保留，恢复时原样显示
        setSniffed([]);
        setSelected(new Set());
        setRequests([]); // 未确认的请求一并作废
      }
    });
    const offSniff = events.browserSniffed.on((list) => {
      setSniffed(list);
      setSelected(new Set());
      setScanning(false);
    });
    // 网页触发的下载拦截 → 确认队列
    const offReq = events.downloadRequest.on((req) => enqueue(req));
    return () => {
      offState();
      offSniff();
      offReq();
      window.clearTimeout(scanTimer.current);
    };
  }, []);

  /** 待确认请求入队，按 URL 去重（嗅探点击与网页拦截可能撞车）。 */
  function enqueue(req: DownloadRequest) {
    setRequests((prev) => (prev.some((r) => r.url === req.url) ? prev : [...prev, req]));
  }

  function rescan() {
    setScanning(true);
    void Browser.scan();
    window.clearTimeout(scanTimer.current);
    scanTimer.current = window.setTimeout(() => setScanning(false), 5000);
  }

  function downloadSelected() {
    for (const s of sniffed) {
      if (s.type === "bili") continue; // B 站条目走专用画质弹窗
      if (selected.has(s.url)) enqueue({ url: s.url, name: s.name, referer: s.page });
    }
    setSelected(new Set());
  }

  /** B 站条目下载完成后的入队反馈。 */
  function biliQueued() {
    setQueued(true);
    window.setTimeout(() => setQueued(false), 3000);
  }

  async function confirmRequest(req: DownloadRequest, name: string) {
    setRequests((prev) => prev.filter((r) => r.url !== req.url));
    try {
      await Browser.download(req.url, name, req.referer);
      setQueued(true);
      window.setTimeout(() => setQueued(false), 3000);
    } catch {
      // 失败详情已由 Go 侧系统对话框提示
    }
  }

  function toggle(url: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(url)) next.delete(url);
      else next.add(url);
      return next;
    });
  }

  const biliEntry = sniffed.find((s) => s.type === "bili");
  const items = sniffed.filter((s) => s.type !== "bili");
  const current = requests[0];

  return (
    <div className="flex h-full flex-col bg-panel text-text">
      {/* 标题栏：可拖动 */}
      <div
        className="flex h-9 shrink-0 items-center justify-between border-b border-border px-3"
        style={{ ["--app-region" as string]: "drag" } as React.CSSProperties}
      >
        <span className="text-xs font-semibold text-muted">资源嗅探</span>
        <div className="flex items-center" style={{ ["--app-region" as string]: "no-drag" } as React.CSSProperties}>
          <IconButton title="重新嗅探" onClick={rescan} disabled={scanning} className="size-6">
            <IconRefresh className={cn("size-3.5", scanning && "animate-spin")} />
          </IconButton>
          <IconButton title="收起（可从工具栏重新打开）" onClick={() => void Browser.hidePanel()} className="size-6">
            <IconX className="size-3.5" />
          </IconButton>
        </div>
      </div>

      {/* B 站视频页专用条目：DASH 分轨嗅探不到整段地址，走画质选择弹窗 */}
      {biliEntry && (
        <div className="flex shrink-0 items-center gap-2 border-b border-border bg-panel-2/40 px-2.5 py-2">
          <span className={cn("shrink-0 rounded px-1 py-0.5 text-[10px] font-medium", TYPE_COLOR.bili)}>
            {TYPE_LABEL.bili}
          </span>
          <span className="min-w-0 flex-1 truncate text-xs font-medium" title={biliEntry.name}>
            {biliEntry.name}
          </span>
          <Button className="h-6 shrink-0 px-2 text-[11px]" onClick={() => setBiliOpen(true)}>
            <IconDownload className="size-3" />
            下载
          </Button>
        </div>
      )}

      {/* 嗅探列表 */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {items.length === 0 && !biliEntry ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-muted">
            <IconSearch className="size-7 opacity-40" />
            <div className="text-xs">暂未嗅探到资源</div>
            <div className="text-[11px] text-muted/70">页面加载完会自动扫描，也可点右上角刷新</div>
          </div>
        ) : (
          items.map((s) => {
            if (s.type === "info") {
              return (
                <div key="info" className="border-b border-border bg-panel-2/40 px-2.5 py-1.5 text-[11px] text-muted">
                  {s.name}
                </div>
              );
            }
            return (
              <label
                key={s.url}
                className="flex cursor-pointer items-center gap-2 border-b border-border px-2.5 py-1.5 transition-colors last:border-b-0 hover:bg-panel-2/60"
              >
                <input
                  type="checkbox"
                  className="size-3.5 accent-[var(--accent)]"
                  checked={selected.has(s.url)}
                  onChange={() => toggle(s.url)}
                />
                <span className={cn("shrink-0 rounded px-1 py-0.5 text-[10px] font-medium", TYPE_COLOR[s.type] ?? TYPE_COLOR.other)}>
                  {TYPE_LABEL[s.type] ?? s.type}
                </span>
                <span className="min-w-0 flex-1 truncate text-xs" title={s.url}>
                  {s.name}
                </span>
                <IconButton
                  title="下载"
                  className="size-6 shrink-0"
                  onClick={(e) => {
                    e.preventDefault();
                    enqueue({ url: s.url, name: s.name, referer: s.page });
                  }}
                >
                  <IconDownload className="size-3.5" />
                </IconButton>
              </label>
            );
          })
        )}
      </div>

      {/* 底栏 */}
      <div className="flex h-10 shrink-0 items-center justify-between border-t border-border px-2.5">
        <span className="text-[11px] text-muted">
          {items.length} 项{selected.size > 0 && ` · 已选 ${selected.size}`}
          {requests.length > 0 && <span className="ml-1.5 text-accent">{requests.length} 个待确认</span>}
          {queued && requests.length === 0 && <span className="ml-1.5 text-success">已入队</span>}
        </span>
        <button
          type="button"
          onClick={downloadSelected}
          disabled={selected.size === 0}
          className="inline-flex h-6 items-center gap-1 rounded-md bg-accent px-2 text-[11px] font-medium text-white hover:opacity-90 disabled:pointer-events-none disabled:opacity-40"
        >
          <IconDownload className="size-3" />
          下载所选
        </button>
      </div>

      {/* 下载确认框：逐个确认，key 保证每个请求的输入状态全新 */}
      {current && (
        <DownloadConfirm
          key={current.url}
          req={current}
          pending={requests.length}
          onConfirm={(name) => void confirmRequest(current, name)}
          onCancel={() => setRequests((prev) => prev.filter((r) => r.url !== current.url))}
        />
      )}

      {/* B 站画质选择弹窗 */}
      {biliOpen && biliEntry && (
        <BilibiliDialog entry={biliEntry} onQueued={biliQueued} onClose={() => setBiliOpen(false)} />
      )}
    </div>
  );
}

/** 文件名确认框：预填建议名，可清空（= 从链接自动推断）。 */
function DownloadConfirm({
  req,
  pending,
  onConfirm,
  onCancel,
}: {
  req: DownloadRequest;
  pending: number;
  onConfirm: (name: string) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(req.name);

  return (
    <Modal
      open
      onClose={onCancel}
      title={pending > 1 ? `保存文件（还有 ${pending - 1} 个待确认）` : "保存文件"}
      width={330}
    >
      <div className="flex flex-col gap-3">
        <div>
          <label className="mb-1 block text-xs text-muted" htmlFor="download-confirm-name">
            文件名
          </label>
          <Input
            id="download-confirm-name"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onFocus={(e) => e.target.select()}
            onKeyDown={(e) => {
              if (e.key === "Enter") onConfirm(name.trim());
            }}
            placeholder="留空则从链接自动推断"
            className="h-8"
          />
        </div>
        <div className="truncate text-xs text-muted/70" title={req.url}>
          {req.url}
        </div>
        <div className="flex items-center justify-end gap-2">
          <Button variant="secondary" onClick={onCancel}>
            取消
          </Button>
          <Button onClick={() => onConfirm(name.trim())}>
            <IconDownload className="size-4" />
            下载
          </Button>
        </div>
      </div>
    </Modal>
  );
}

/** 哔哩哔哩画质选择框：DASH 分轨下载（视频+音频自动合流）或仅提取音轨。 */
function BilibiliDialog({
  entry,
  onQueued,
  onClose,
}: {
  entry: Sniffed;
  onQueued: () => void;
  onClose: () => void;
}) {
  const [info, setInfo] = useState<BilibiliInfo | null>(null);
  const [error, setError] = useState("");
  const [quality, setQuality] = useState(0);
  const [format, setFormat] = useState("mp4");
  const [name, setName] = useState(entry.name);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    Browser.bilibiliInfo()
      .then((v) => {
        if (!alive) return;
        setInfo(v);
        if (v.qualities.length > 0) setQuality(v.qualities[0].id); // 默认最高画质
      })
      .catch((e: unknown) => {
        if (alive) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      alive = false;
    };
  }, []);

  async function submit() {
    setBusy(true);
    setError("");
    try {
      await Browser.bilibiliDownload(quality, name.trim(), format);
      onQueued();
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  }

  const selectCls =
    "h-8 w-full rounded-md border border-border bg-panel px-2 text-sm text-text focus:border-accent focus:outline-none";

  return (
    <Modal open onClose={onClose} title="哔哩哔哩视频下载" width={352}>
      <div className="flex flex-col gap-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-medium" title={entry.name}>
            {entry.name}
          </div>
          {info && (
            <div className="mt-0.5 text-[11px] text-muted">
              {info.pages > 1 && `P${info.page}/${info.pages} · `}
              {info.dash ? "DASH 分轨，视频+音频自动合流" : "旧版单文件直链"}
              {info.dash && info.audio && ` · ${info.audio}`}
            </div>
          )}
        </div>
        {!info && !error && <div className="text-xs text-muted">正在读取画质信息…</div>}
        {error && <div className="rounded-md bg-danger/10 px-2 py-1.5 text-xs text-danger">{error}</div>}
        {info && (
          <>
            {info.dash ? (
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted">画质</span>
                <select
                  value={quality}
                  onChange={(e) => setQuality(Number(e.target.value))}
                  className={selectCls}
                >
                  {info.qualities.map((q) => (
                    <option key={q.id} value={q.id}>
                      {q.desc}
                      {q.size > 0 ? ` · 约 ${formatBytes(q.size)}` : ""}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              info.qualities[0] && (
                <div className="text-[11px] text-muted">
                  原始画质{info.qualities[0].size > 0 ? ` · ${formatBytes(info.qualities[0].size)}` : ""}
                </div>
              )
            )}
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted">格式</span>
              <select value={format} onChange={(e) => setFormat(e.target.value)} className={selectCls}>
                <optgroup label="视频容器">
                  <option value="mp4">MP4</option>
                  <option value="mkv">MKV</option>
                  <option value="mov">MOV</option>
                  <option value="webm">WebM（可能需转码）</option>
                </optgroup>
                <optgroup label="仅提取音频">
                  <option value="m4a">M4A</option>
                  <option value="mp3">MP3</option>
                  <option value="flac">FLAC</option>
                </optgroup>
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted">文件名</span>
              <Input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                onFocus={(e) => e.target.select()}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !busy) void submit();
                }}
                className="h-8"
              />
            </label>
          </>
        )}
        <div className="flex items-center justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={!info || busy}>
            <IconDownload className="size-4" />
            {busy ? "正在入队…" : "下载"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
