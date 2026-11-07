import { useEffect, useRef, useState } from "react";
import { Browser, events, type BrowserState } from "../mygo";
import { IconArrowLeft, IconArrowRight, IconExternal, IconRefresh, IconSearch, IconX } from "../components/icons";
import { IconButton } from "../components/ui";
import { cn } from "../lib/utils";

/**
 * 浏览模式常驻工具栏：导航与嗅探面板开关都在这里，
 * 不依赖嗅探浮窗是否打开。高度 h-11 = 44 CSS px，
 * 与 Go 侧 browserToolbarH 一致（站点窗口对齐到工具栏下方）。
 */
export function BrowserToolbar() {
  const [state, setState] = useState<BrowserState>({
    mode: "browsing",
    url: "",
    title: "",
    canBack: false,
    canForward: false,
    loading: false,
    panelVisible: true,
  });
  const [address, setAddress] = useState("");
  // 地址输入只在站点地址真正变化时同步，避免事件广播覆盖正在输入的内容。
  const lastUrl = useRef("");

  useEffect(() => {
    const apply = (s: BrowserState) => {
      setState(s);
      if (s.url !== lastUrl.current) {
        lastUrl.current = s.url;
        setAddress(s.url);
      }
    };
    void Browser.state().then(apply);
    return events.browserState.on(apply);
  }, []);

  function go() {
    if (address.trim()) void Browser.navigate(address);
  }

  return (
    <div className="flex h-11 shrink-0 items-center gap-1 border-b border-border bg-panel px-2">
      <IconButton title="后退" disabled={!state.canBack} onClick={() => void Browser.back()}>
        <IconArrowLeft className="size-4" />
      </IconButton>
      <IconButton title="前进" disabled={!state.canForward} onClick={() => void Browser.forward()}>
        <IconArrowRight className="size-4" />
      </IconButton>
      {state.loading ? (
        <IconButton title="停止" onClick={() => void Browser.stop()}>
          <IconX className="size-4" />
        </IconButton>
      ) : (
        <IconButton title="重新加载" onClick={() => void Browser.reload()}>
          <IconRefresh className="size-4" />
        </IconButton>
      )}

      <input
        value={address}
        onChange={(e) => setAddress(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && go()}
        placeholder="输入网址，回车前往"
        spellCheck={false}
        className={cn(
          "h-7 min-w-0 flex-1 rounded-md border border-border bg-bg px-2.5 text-xs text-text",
          "placeholder:text-muted/70 focus:border-accent focus:outline-none",
        )}
      />

      <button
        type="button"
        onClick={() => void (state.panelVisible ? Browser.hidePanel() : Browser.showPanel())}
        title={state.panelVisible ? "收起嗅探面板" : "打开嗅探面板"}
        className={cn(
          "inline-flex h-7 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors",
          state.panelVisible
            ? "bg-accent-soft text-accent"
            : "text-muted hover:bg-panel-2 hover:text-text",
        )}
      >
        <IconSearch className="size-3.5" />
        嗅探
      </button>
      <IconButton
        title="在系统浏览器打开"
        disabled={!state.url}
        onClick={() => void Browser.openExternal(state.url)}
      >
        <IconExternal className="size-4" />
      </IconButton>
      <button
        type="button"
        onClick={() => void Browser.exit()}
        title="退出浏览，回到应用界面"
        className="inline-flex h-7 shrink-0 items-center rounded-md border border-border px-2 text-xs text-muted transition-colors hover:bg-panel-2 hover:text-text"
      >
        退出
      </button>
    </div>
  );
}
