import { useEffect, useState } from "react";
import { ArrowDownToLine, Monitor, Apple } from "lucide-react";
import { useReveal } from "../lib/useReveal";

const REPO = "https://github.com/besscroft/Knot";
const RELEASES = `${REPO}/releases/latest`;
const INFO_ENDPOINT = "/download/latest/info";

const PLATFORMS = [
  {
    icon: Monitor,
    name: "Windows",
    desc: "Windows 10+（需 WebView2）",
    asset: "exe 安装包 · NSIS",
    href: "/download/latest/windows",
    variants: [
      { label: "便携版 zip", href: "/download/latest/windows-portable" },
      { label: "ARM64", href: "/download/latest/windows-arm64" },
    ],
  },
  {
    icon: Apple,
    name: "macOS",
    desc: "Apple Silicon / Intel 通用",
    asset: "dmg 镜像 · .app",
    href: "/download/latest/macos",
    variants: [{ label: "zip 便携版", href: "/download/latest/macos-zip" }],
  },
];

export function Download() {
  const panelRef = useReveal<HTMLDivElement>();
  const [version, setVersion] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch(INFO_ENDPOINT)
      .then((res) => (res.ok ? res.json() : null))
      .then((data: { version?: string } | null) => {
        if (!cancelled && data?.version) {
          setVersion(data.version);
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

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
              {version && (
                <span className="ml-2 inline-block rounded-full border border-line bg-raised px-2.5 py-0.5 align-middle text-xs font-normal text-secondary">
                  v{version}
                </span>
              )}
            </h2>
            <p className="mx-auto mt-3 max-w-md text-secondary">
              免费开源，安装包发布于 GitHub
              Releases，官网提供加速下载，国内直连更快。
            </p>
            <div className="mx-auto mt-8 grid max-w-2xl gap-4 sm:grid-cols-2">
              {PLATFORMS.map((platform) => (
                <div
                  key={platform.name}
                  className="pressable group relative rounded-lg border border-line bg-raised p-5 text-left hover:border-accent"
                >
                  {/* 拉伸链接：整张卡片可点，变体小链接需在其上层 */}
                  <a
                    href={platform.href}
                    className="absolute inset-0 rounded-lg"
                    aria-label={`下载 ${platform.name} 版 Knot`}
                  />
                  <div className="flex items-center justify-between">
                    <span className="flex size-10 items-center justify-center rounded-lg bg-accent-subtle">
                      <platform.icon className="size-5 text-accent-strong" />
                    </span>
                    <ArrowDownToLine className="size-4 text-muted transition-transform group-hover:translate-y-0.5 group-hover:text-accent" />
                  </div>
                  <div className="mt-4 font-medium">{platform.name}</div>
                  <div className="mt-1 text-sm text-secondary">{platform.desc}</div>
                  <div className="mt-2 text-xs text-muted">{platform.asset}</div>
                  <div className="relative z-10 mt-2 flex gap-3 text-xs">
                    {platform.variants.map((variant) => (
                      <a
                        key={variant.href}
                        href={variant.href}
                        className="text-muted underline-offset-2 hover:text-accent hover:underline"
                      >
                        {variant.label}
                      </a>
                    ))}
                  </div>
                </div>
              ))}
            </div>
            <p className="mt-6 text-xs text-muted">
              首次下载 YouTube 视频时会自动获取 yt-dlp 与 FFmpeg
              并校验，也可以在设置中手动指定路径。
            </p>
            <p className="mt-2 text-xs text-muted">
              也可以前往{" "}
              <a
                href={RELEASES}
                target="_blank"
                rel="noreferrer"
                className="underline-offset-2 hover:text-accent hover:underline"
              >
                GitHub Releases
              </a>{" "}
              查看历史版本与 SHA256 校验文件。
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
