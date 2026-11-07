import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
// /#panel 是右上角嗅探浮窗（frameless 子窗口），其余是主应用界面。
const isPanel = window.location.hash.includes("panel");

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
