import { GithubIcon } from "./icons";

const REPO = "https://github.com/besscroft/Knot";

export function Footer() {
  return (
    <footer className="border-t border-line-subtle py-10">
      <div className="mx-auto flex max-w-5xl flex-col items-center gap-3 px-5 text-sm text-muted">
        <div className="flex items-center gap-2">
          <img src="/favicon.svg" alt="" className="size-5" />
          <span className="font-medium text-secondary">Knot</span>
        </div>
        <p>跨平台资源下载器 · 基于 MyGo 构建 · 前端由 Vite+ 驱动</p>
        <a
          href={REPO}
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-1.5 transition-colors hover:text-foreground"
        >
          <GithubIcon className="size-4" />
          github.com/besscroft/Knot
        </a>
        <p className="text-xs">MIT License</p>
      </div>
    </footer>
  );
}
