import { useEffect, useRef, useState } from "react";
import { Browser, events, type BrowserState, type Sniffed } from "../mygo";
import { IconArrowLeft, IconArrowRight, IconDownload, IconRefresh, IconSearch, IconX } from "../components/icons";
import { IconButton } from "../components/ui";
import { cn } from "../lib/utils";

const TYPE_LABEL: Record<string, string> = {
  info: "提示",
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
  hls: "text-danger bg-danger/10",
  video: "text-violet-500 bg-violet-500/10",
  audio: "text-cyan-500 bg-cyan-500/10",
  image: "text-accent bg-accent-soft",
  archive: "text-orange-500 bg-orange-500/10",
  document: "text-success bg-success/10",
  link: "text-muted bg-panel-2",
  other: "text-muted bg-panel-2",
};

/** 右上角嗅探浮窗：主窗口浏览网站时悬浮展示可下载资源。 */
export function SniffPanel() {
  const [state, setState] = useState<BrowserState>({ mode: "browsing", url: "", title: "", canBack: false, canForward: false });
  const [sniffed, setSniffed] = useState<Sniffed[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [address, setAddress] = useState("");
  const [scanning, setScanning] = useState(false);
  const [queued, setQueued] = useState(false);
  const scanTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    void Browser.state().then((s) => {
      setState(s);
      setAddress(s.url);
    });
    void Browser.scan(); // 打开浮窗时补一次嗅探
    const offState = events.browserState.on((s) => {
      setState(s);
      setAddress(s.url);
      if (s.mode !== "browsing") {
        setSniffed([]);
        setSelected(new Set());
      }
    });
    const offSniff = events.browserSniffed.on((list) => {
      setSniffed(list);
      setSelected(new Set());
      setScanning(false);
    });
    return () => {
      offState();
      offSniff();
      window.clearTimeout(scanTimer.current);
    };
  }, []);

  function go() {
    if (address.trim()) void Browser.navigate(address);
  }

  function rescan() {
    setScanning(true);
    void Browser.scan();
    window.clearTimeout(scanTimer.current);
    scanTimer.current = window.setTimeout(() => setScanning(false), 5000);
  }

  async function downloadSelected() {
    for (const s of sniffed) {
      if (selected.has(s.url)) await Browser.download(s.url, s.name, s.page);
    }
    setSelected(new Set());
    setQueued(true);
    window.setTimeout(() => setQueued(false), 3000);
  }

  function toggle(url: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(url)) next.delete(url);
      else next.add(url);
      return next;
    });
  }

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
          <IconButton title="收起" onClick={() => void Browser.hidePanel()} className="size-6">
            <IconX className="size-3.5" />
          </IconButton>
        </div>
      </div>

      {/* 地址与导航 */}
      <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-2">
        <IconButton title="后退" disabled={!state.canBack} onClick={() => void Browser.back()} className="size-6">
          <IconArrowLeft className="size-3.5" />
        </IconButton>
        <IconButton title="前进" disabled={!state.canForward} onClick={() => void Browser.forward()} className="size-6">
          <IconArrowRight className="size-3.5" />
        </IconButton>
        <input
          value={address}
          onChange={(e) => setAddress(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && go()}
          placeholder="输入网址"
          className="h-6 min-w-0 flex-1 rounded border border-border bg-bg px-1.5 text-xs text-text focus:border-accent focus:outline-none"
        />
        <IconButton title="停止" onClick={() => void Browser.stop()} className="size-6">
          <IconX className="size-3.5" />
        </IconButton>
        <IconButton title="前往" onClick={go} className="size-6">
          <IconSearch className="size-3.5" />
        </IconButton>
      </div>

      {/* 嗅探列表 */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {sniffed.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-muted">
            <IconSearch className="size-7 opacity-40" />
            <div className="text-xs">暂未嗅探到资源</div>
            <div className="text-[11px] text-muted/70">页面加载完会自动扫描，也可点右上角刷新</div>
          </div>
        ) : (
          sniffed.map((s) => {
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
                    void Browser.download(s.url, s.name, s.page).then(() => {
                      setQueued(true);
                      window.setTimeout(() => setQueued(false), 3000);
                    });
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
          {sniffed.length} 项{selected.size > 0 && ` · 已选 ${selected.size}`}
          {queued && <span className="ml-1.5 text-success">已入队</span>}
        </span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => void downloadSelected()}
            disabled={selected.size === 0}
            className="inline-flex h-6 items-center gap-1 rounded-md bg-accent px-2 text-[11px] font-medium text-white hover:opacity-90 disabled:pointer-events-none disabled:opacity-40"
          >
            <IconDownload className="size-3" />
            下载所选
          </button>
          <button
            type="button"
            onClick={() => void Browser.exit()}
            className="inline-flex h-6 items-center rounded-md border border-border px-2 text-[11px] text-muted hover:bg-panel-2 hover:text-text"
          >
            退出浏览
          </button>
        </div>
      </div>
    </div>
  );
}
