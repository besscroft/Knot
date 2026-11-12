import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
// /#panel 是右上角嗅探浮窗（frameless 子窗口），其余是主应用界面。
const isPanel = window.location.hash.includes("panel");

// 深浅色实时跟随系统（index.html 已在首帧前设定初始 data-theme）。
const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");
const applyTheme = (query: MediaQueryList | MediaQueryListEvent) => {
  document.documentElement.dataset.theme = query.matches ? "dark" : "light";
};
applyTheme(darkQuery);
darkQuery.addEventListener("change", applyTheme);

async function mount() {
  const root = createRoot(document.getElementById("root")!);
  if (isPanel) {
    const { SniffPanel } = await import("./views/SniffPanel");
    root.render(
      <StrictMode>
        <SniffPanel />
      </StrictMode>,
    );
  } else {
    const { default: App } = await import("./App");
    root.render(
      <StrictMode>
        <App />
      </StrictMode>,
    );
  }
}

void mount();
