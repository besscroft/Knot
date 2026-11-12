import { useEffect, useState } from "react";
import { Globe } from "lucide-react";
import { Browser } from "../mygo";
import { Button } from "../components/arc/button/button";
import { Input } from "../components/arc/input/input";

/** 浏览器视图（启动器）：主窗口随即进入浏览模式，嗅探面板浮出在右上角。 */
export function BrowserView() {
  const [address, setAddress] = useState("");

  useEffect(() => {
    // 若浏览仍在进行（例如从浮窗退出前主窗口刷新），同步状态展示。
    void Browser.state().then((s) => {
      if (s.mode === "browsing" && s.url) setAddress(s.url);
    });
  }, []);

  function start() {
    void Browser.open(address);
  }

  return (
    <div className="flex h-full flex-col items-center justify-center p-6">
      <div className="w-full max-w-md">
        {/* 聚焦头部：图标容器与 EmptyState 同语言，标题与主描述居中 */}
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          <div className="flex size-14 items-center justify-center rounded-[var(--radius-panel)] border border-border bg-surface-muted text-accent">
            <Globe className="size-7" />
          </div>
          <h1 className="text-lg font-semibold">打开网站</h1>
          <p className="text-xs leading-5 text-muted">
            进入网站后，页面里的图片、音视频、压缩包等资源会被自动收集，勾选即可下载
          </p>
        </div>
        <Input
          label="网址"
          autoFocus
          value={address}
          onChange={(e) => setAddress(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && start()}
          placeholder="输入网址，如 example.com"
        />
        <Button className="mt-3 w-full" onClick={start}>
          开始浏览
        </Button>
        <p className="mt-4 text-center text-[11px] leading-5 text-muted/70">
          网页触发的下载会自动转入下载队列；退出浏览随时回到应用界面
        </p>
      </div>
    </div>
  );
}
