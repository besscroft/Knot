import { GithubIcon } from "./icons";

const REPO = "https://github.com/besscroft/Knot";

export function Nav() {
  return (
    <header className="fixed inset-x-0 top-0 z-50 border-b border-line-subtle bg-background/80 backdrop-blur-md">
      <div className="mx-auto flex h-14 max-w-5xl items-center justify-between px-5">
        <a href="#" className="flex items-center gap-2.5">
          <img src="/favicon.svg" alt="Knot 标志" className="size-6" />
          <span className="text-[15px] font-semibold tracking-tight">Knot</span>
        </a>
        <nav className="flex items-center gap-1 text-sm text-secondary">
          <a
            href="#features"
            className="rounded-lg px-3 py-1.5 transition-colors hover:bg-surface-muted hover:text-foreground"
          >
            功能
          </a>
          <a
            href="#download"
            className="rounded-lg px-3 py-1.5 transition-colors hover:bg-surface-muted hover:text-foreground"
          >
            下载
          </a>
          <a
            href={REPO}
            target="_blank"
            rel="noreferrer"
            className="pressable ml-1 flex items-center gap-1.5 rounded-lg border border-line-strong px-3 py-1.5 hover:border-accent hover:text-foreground"
          >
            <GithubIcon className="size-4" />
            GitHub
          </a>
        </nav>
      </div>
    </header>
  );
}
