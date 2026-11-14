import type { CSSProperties } from "react";
import { Download, FileVideo, Globe, Music, Settings } from "lucide-react";

const REPO = "https://github.com/besscroft/Knot";

function MockWindow() {
  const tasks = [
    { icon: FileVideo, name: "湖边日出 · 4K.mp4", chip: "MP4", progress: 0.82, meta: "24.6 MB/s · 8 分片", active: true },
    { icon: Music, name: "Radio Mix.mp3", chip: "MP3", progress: 0.37, meta: "8.1 MB/s · 已完成转封装", active: true },
    { icon: Globe, name: "城市之光 · 1080P.m3u8", chip: "HLS", progress: 1, meta: "已合并为 MP4", active: false },
  ];

  return (
    <div className="enter relative mx-auto mt-16 max-w-3xl overflow-hidden rounded-2xl border border-line bg-raised text-left shadow-[0_1px_2px_rgba(0,0,0,0.06),0_8px_24px_-12px_rgba(0,0,0,0.12)] dark:shadow-[0_1px_2px_rgba(0,0,0,0.4),0_8px_24px_-12px_rgba(0,0,0,0.5)]" style={{ animationDelay: "300ms" }}>
      {/* 标题栏 */}
      <div className="flex h-10 items-center border-b border-line-subtle px-4">
        <div className="flex gap-1.5">
          <span className="size-2.5 rounded-full bg-line-strong" />
          <span className="size-2.5 rounded-full bg-line-strong" />
          <span className="size-2.5 rounded-full bg-line-strong" />
        </div>
        <span className="flex-1 text-center text-xs text-muted">Knot</span>
        <span className="w-11" />
      </div>
      <div className="grid grid-cols-[9.5rem_1fr] sm:grid-cols-[11rem_1fr]">
        {/* 侧栏 */}
        <div className="border-r border-line-subtle p-2.5 text-[13px] text-secondary">
          {[
            { icon: Download, label: "全部任务", active: true },
            { icon: Globe, label: "浏览中" },
            { icon: Settings, label: "设置" },
          ].map((item) => (
            <div
              key={item.label}
              className={`flex items-center gap-2 rounded-lg px-2.5 py-1.5 ${
                item.active ? "bg-accent-subtle font-medium text-accent-strong" : ""
              }`}
            >
              <item.icon className="size-3.5" />
              {item.label}
            </div>
          ))}
        </div>
        {/* 任务列表 */}
        <div className="divide-y divide-line-subtle">
          {tasks.map((task) => (
            <div key={task.name} className="flex items-center gap-3 px-4 py-3">
              <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-surface-muted">
                <task.icon className="size-4 text-secondary" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-[13px] font-medium">{task.name}</span>
                  <span className="shrink-0 rounded-md bg-accent-subtle px-1.5 py-0.5 text-[10px] font-medium text-accent-strong">
                    {task.chip}
                  </span>
                </div>
                <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-surface-muted">
                  <div
                    className={`bar h-full w-full rounded-full ${task.active ? "bg-accent" : "bg-line-strong"}`}
                    style={{ "--p": task.progress } as CSSProperties}
                  />
                </div>
                <div className="mt-1 text-[11px] text-muted">{task.meta}</div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function Hero() {
  return (
    <section className="relative overflow-hidden pt-32 pb-20">
      <div className="relative mx-auto max-w-5xl px-5">
        <div className="mx-auto max-w-3xl text-center">
          <span className="enter inline-flex items-center gap-1.5 rounded-full border border-line-subtle bg-surface-muted px-3 py-1 text-xs text-secondary">
            开源免费 · MIT License
          </span>
          <h1
            className="enter mt-6 text-5xl font-semibold tracking-tight sm:text-6xl"
            style={{ animationDelay: "60ms" }}
          >
            你的下载<span className="text-accent">代理人</span>
          </h1>
          <p
            className="enter mx-auto mt-5 max-w-xl text-lg leading-relaxed text-secondary text-pretty"
            style={{ animationDelay: "120ms" }}
          >
            跨平台资源下载器。HTTP 直链、HLS (m3u8)、YouTube
            一站式下载，内置网页浏览、资源嗅探与浏览器插件，自动避开防盗链。
          </p>
          <div
            className="enter mt-8 flex flex-wrap items-center justify-center gap-3"
            style={{ animationDelay: "180ms" }}
          >
            <a
              href="#download"
              className="pressable inline-flex items-center gap-2 rounded-xl bg-brand px-5 py-2.5 text-sm font-medium text-white hover:bg-brand-strong"
            >
              <Download className="size-4" />
              下载 Knot
            </a>
            <a
              href={REPO}
              target="_blank"
              rel="noreferrer"
              className="pressable inline-flex items-center gap-2 rounded-xl border border-line-strong px-5 py-2.5 text-sm font-medium hover:border-accent"
            >
              查看源码
            </a>
          </div>
          <div
            className="enter mt-7 flex flex-wrap items-center justify-center gap-x-5 gap-y-1.5 text-xs text-muted"
            style={{ animationDelay: "240ms" }}
          >
            <span>Windows 10+</span>
            <span aria-hidden>·</span>
            <span>macOS (Universal)</span>
            <span aria-hidden>·</span>
            <span>Go + React</span>
            <span aria-hidden>·</span>
            <span>系统 WebView · 无 cgo</span>
          </div>
        </div>
        <MockWindow />
      </div>
    </section>
  );
}
