import {
  CirclePlay,
  FileCog,
  Link,
  ListChecks,
  MonitorPlay,
  Puzzle,
  Radar,
  ShieldCheck,
  SunMoon,
} from "lucide-react";
import { useReveal } from "../lib/useReveal";

const FEATURES = [
  {
    icon: Link,
    title: "HTTP 直链下载",
    desc: "多线程分片（默认 8 段）、断点续传、失败自动重试，并发上限可配。",
  },
  {
    icon: MonitorPlay,
    title: "HLS 视频流 (m3u8)",
    desc: "master 播放列表自动选最高清晰度，分片并发 + AES-128 解密，完成后自动转封装为 MP4。",
  },
  {
    icon: CirclePlay,
    title: "YouTube 下载",
    desc: "基于 yt-dlp，粘贴链接即可选画质（含高帧率标注）或仅提取音频，首次使用自动准备运行环境。",
  },
  {
    icon: Radar,
    title: "应用内浏览 + 资源嗅探",
    desc: "边浏览边自动收集网页媒体资源，勾选即下；自动携带站点的 Cookie / Referer / UA 规避防盗链。",
  },
  {
    icon: Puzzle,
    title: "浏览器插件",
    desc: "Chromium 系浏览器一键安装，日常浏览器里嗅探资源、右键交给 Knot；B 站视频页支持清晰度选择与 DASH 合流。",
  },
  {
    icon: FileCog,
    title: "格式选择与转换",
    desc: "保存为 MP4 / MKV / MOV / WebM，或提取音频为 M4A / MP3 / Opus / FLAC / WAV —— FFmpeg 流拷贝，秒级完成。",
  },
];

const MORE = [
  { icon: ListChecks, label: "下载队列：暂停 / 继续 / 删除，任务栏实时进度" },
  { icon: ShieldCheck, label: "下载代理：http / https / socks5，留空跟随系统" },
  { icon: SunMoon, label: "深浅色跟随系统，单实例锁，类型安全 IPC" },
];

export function Features() {
  const headerRef = useReveal<HTMLDivElement>();
  const gridRef = useReveal<HTMLDivElement>();
  const moreRef = useReveal<HTMLDivElement>();

  return (
    <section id="features" className="scroll-mt-20 py-20">
      <div className="mx-auto max-w-5xl px-5">
        <div ref={headerRef} className="stagger max-w-xl">
          <span className="text-sm font-medium text-accent">功能</span>
          <h2 className="mt-2 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
            一个应用，覆盖常见下载场景
          </h2>
          <p className="mt-3 leading-relaxed text-secondary">
            直链、流媒体、网页资源统统接得住，下载过程对防盗链和清晰度选择的处理都替你想好了。
          </p>
        </div>
        <div ref={gridRef} className="stagger mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {FEATURES.map((feature) => (
            <div
              key={feature.title}
              className="rounded-2xl border border-line-subtle bg-raised p-5 transition-colors hover:border-accent/40"
            >
              <span className="flex size-10 items-center justify-center rounded-lg bg-accent-subtle">
                <feature.icon className="size-5 text-accent-strong" />
              </span>
              <h3 className="mt-4 font-medium">{feature.title}</h3>
              <p className="mt-1.5 text-sm leading-relaxed text-secondary">{feature.desc}</p>
            </div>
          ))}
        </div>
        <div
          ref={moreRef}
          className="stagger mt-4 grid gap-x-8 gap-y-3 rounded-2xl border border-line-subtle bg-surface-muted/60 p-5 text-sm text-secondary sm:grid-cols-1 lg:grid-cols-3"
        >
          {MORE.map((item) => (
            <div key={item.label} className="flex items-center gap-2.5">
              <item.icon className="size-4 shrink-0 text-accent" />
              {item.label}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
