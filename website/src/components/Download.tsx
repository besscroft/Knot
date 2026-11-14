import { ArrowDownToLine, Monitor, Apple } from "lucide-react";
import { useReveal } from "../lib/useReveal";

const REPO = "https://github.com/besscroft/Knot";
const RELEASES = `${REPO}/releases/latest`;

const PLATFORMS = [
  {
    icon: Monitor,
    name: "Windows",
    desc: "Windows 10+（需 WebView2）",
    asset: "exe 安装包 · NSIS",
    href: RELEASES,
  },
  {
    icon: Apple,
    name: "macOS",
    desc: "Apple Silicon / Intel 通用",
    asset: "dmg 镜像 · .app",
    href: RELEASES,
  },
];

export function Download() {
  const panelRef = useReveal<HTMLDivElement>();

  return (
    <section id="download" className="scroll-mt-20 py-20">
      <div className="mx-auto max-w-5xl px-5">
        <div
          ref={panelRef}
          className="stagger relative overflow-hidden rounded-2xl border border-line-subtle bg-surface-muted/60 px-6 py-14 text-center"
        >
          <div className="relative">
            <h2 className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
              下载 Knot
            </h2>
            <p className="mx-auto mt-3 max-w-md text-secondary">
              免费开源，安装包来自 GitHub Releases。
            </p>
            <div className="mx-auto mt-8 grid max-w-2xl gap-4 sm:grid-cols-2">
              {PLATFORMS.map((platform) => (
                <a
                  key={platform.name}
                  href={platform.href}
                  target="_blank"
                  rel="noreferrer"
                  className="pressable group rounded-lg border border-line bg-raised p-5 text-left hover:border-accent"
                >
                  <div className="flex items-center justify-between">
                    <span className="flex size-10 items-center justify-center rounded-lg bg-accent-subtle">
                      <platform.icon className="size-5 text-accent-strong" />
                    </span>
                    <ArrowDownToLine className="size-4 text-muted transition-transform group-hover:translate-y-0.5 group-hover:text-accent" />
                  </div>
                  <div className="mt-4 font-medium">{platform.name}</div>
                  <div className="mt-1 text-sm text-secondary">{platform.desc}</div>
                  <div className="mt-2 text-xs text-muted">{platform.asset}</div>
                </a>
              ))}
            </div>
            <p className="mt-6 text-xs text-muted">
              首次下载 YouTube 视频时会自动获取 yt-dlp 与 FFmpeg
              并校验，也可以在设置中手动指定路径。
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
