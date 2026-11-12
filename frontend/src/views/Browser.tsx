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
    <div className="flex h-full items-center justify-center p-6">
      <div className="w-full max-w-md">
        <div className="mb-6 flex items-center gap-2.5">
          <Globe className="size-5 text-accent" />
          <h1 className="text-lg font-semibold">打开网站</h1>
        </div>
        <div className="flex items-end gap-2">
          <Input
            label="网址"
            className="min-w-0 flex-1"
            autoFocus
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && start()}
            placeholder="输入网址，如 example.com"
          />
          <Button onClick={start}>开始浏览</Button>
        </div>
        <p className="mt-4 text-xs leading-5 text-muted">
          主窗口将直接进入网站，右上角浮出「资源嗅探」面板：页面里的图片、音视频、压缩包等资源会被自动收集，勾选即可下载；
          网页触发的下载也会自动转入下载队列。退出浏览随时回到应用界面。
        </p>
      </div>
    </div>
  );
}
