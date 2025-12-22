"use client";

import { useEffect, useRef, useState } from "react";

type StackEntry = {
  tag: string;
  id?: string;
  className?: string;
  href?: string;
  position?: string;
  zIndex?: string;
  pointerEvents?: string;
};

type HitInfo = {
  x: number;
  y: number;
  target?: StackEntry;
  top?: StackEntry;
  link?: StackEntry;
  stack: StackEntry[];
  blocked: boolean;
};

const MAX_STACK = 6;
const STORAGE_KEY = "bankai:debug:clicks";

const toEntry = (el: Element | null): StackEntry | undefined => {
  if (!el || !(el instanceof HTMLElement)) {
    return undefined;
  }
  const styles = window.getComputedStyle(el);
  const href = el instanceof HTMLAnchorElement ? el.href : undefined;
  return {
    tag: el.tagName.toLowerCase(),
    id: el.id || undefined,
    className: el.className || undefined,
    href,
    position: styles.position,
    zIndex: styles.zIndex,
    pointerEvents: styles.pointerEvents,
  };
};

const formatEntry = (entry?: StackEntry) => {
  if (!entry) return "none";
  const id = entry.id ? `#${entry.id}` : "";
  const cls = entry.className ? `.${String(entry.className).split(" ").filter(Boolean).slice(0, 2).join(".")}` : "";
  return `${entry.tag}${id}${cls}`;
};

export function ClickOverlayDetector() {
  const [enabled, setEnabled] = useState(false);
  const [lastHit, setLastHit] = useState<HitInfo | null>(null);
  const lastHighlightRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const stored = window.localStorage.getItem(STORAGE_KEY);
    const initial = params.has("debugClicks") || stored === "1";
    setEnabled(initial);

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.ctrlKey && event.shiftKey && event.key.toLowerCase() === "d") {
        const next = !enabled;
        window.localStorage.setItem(STORAGE_KEY, next ? "1" : "0");
        setEnabled(next);
        if (!next && lastHighlightRef.current) {
          lastHighlightRef.current.removeAttribute("data-debug-click-blocker");
          lastHighlightRef.current = null;
        }
        console.info(`[debug] click detector ${next ? "enabled" : "disabled"}`);
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const onPointerDown = (event: PointerEvent) => {
      if (event.button !== 0) {
        return;
      }
      const x = event.clientX;
      const y = event.clientY;
      const stack = document.elementsFromPoint(x, y);
      const top = stack[0] as HTMLElement | undefined;
      const target = event.target as HTMLElement | null;
      const link = target?.closest("a") as HTMLElement | null;
      const blocked = Boolean(link && top && !link.contains(top));

      if (lastHighlightRef.current && lastHighlightRef.current !== top) {
        lastHighlightRef.current.removeAttribute("data-debug-click-blocker");
      }
      if (top) {
        top.setAttribute("data-debug-click-blocker", "true");
        lastHighlightRef.current = top;
      }

      setLastHit({
        x,
        y,
        target: toEntry(target),
        top: toEntry(top ?? null),
        link: toEntry(link),
        stack: stack.slice(0, MAX_STACK).map((el) => toEntry(el)).filter(Boolean) as StackEntry[],
        blocked,
      });

      if (blocked) {
        console.warn("[debug] click blocked by overlay", {
          x,
          y,
          top: toEntry(top ?? null),
          link: toEntry(link),
        });
      }
    };

    window.addEventListener("pointerdown", onPointerDown, true);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown, true);
      if (lastHighlightRef.current) {
        lastHighlightRef.current.removeAttribute("data-debug-click-blocker");
        lastHighlightRef.current = null;
      }
    };
  }, [enabled]);

  if (!enabled) {
    return null;
  }

  return (
    <div className="fixed right-3 top-3 z-[9999] w-[320px] rounded-md border border-yellow-400/60 bg-black/80 p-3 text-[11px] text-yellow-100 shadow-xl">
      <div className="flex items-center justify-between pb-2">
        <span className="font-mono uppercase tracking-widest text-[10px]">
          Click Detector
        </span>
        <button
          type="button"
          className="rounded border border-yellow-400/40 px-2 py-0.5 text-[10px] hover:bg-yellow-500/20"
          onClick={() => {
            window.localStorage.setItem(STORAGE_KEY, "0");
            setEnabled(false);
          }}
        >
          Hide
        </button>
      </div>
      <div className="space-y-1 font-mono">
        <div>last: {lastHit ? `${lastHit.x},${lastHit.y}` : "none"}</div>
        <div>target: {formatEntry(lastHit?.target)}</div>
        <div>top: {formatEntry(lastHit?.top)}</div>
        <div>link: {formatEntry(lastHit?.link)}</div>
        <div>blocked: {lastHit?.blocked ? "yes" : "no"}</div>
      </div>
      <div className="pt-2 text-[10px] text-yellow-200/70">
        Stack (top →):
      </div>
      <div className="max-h-32 overflow-y-auto pt-1 font-mono text-[10px] text-yellow-100/80">
        {lastHit?.stack?.length ? (
          lastHit.stack.map((entry, idx) => (
            <div key={`${entry.tag}-${idx}`}>
              {idx + 1}. {formatEntry(entry)}{" "}
              <span className="text-yellow-200/60">
                {entry.position}/{entry.zIndex}/{entry.pointerEvents}
              </span>
            </div>
          ))
        ) : (
          <div>none</div>
        )}
      </div>
      <div className="pt-2 text-[10px] text-yellow-200/70">
        Toggle with Ctrl+Shift+D
      </div>
    </div>
  );
}
