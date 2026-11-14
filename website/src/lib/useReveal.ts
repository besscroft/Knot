import { useEffect, useRef } from "react";

/**
 * 滚动 reveal：元素进入视口（底部上留 80px 缓冲）时加一次 is-visible。
 * 配合 index.css 的 .stagger 使用；只触发一次，动画可被中断不回放。
 */
export function useReveal<T extends HTMLElement>() {
  const ref = useRef<T>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          el.classList.add("is-visible");
          io.disconnect();
        }
      },
      { rootMargin: "0px 0px -80px 0px", threshold: 0.1 },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);

  return ref;
}
