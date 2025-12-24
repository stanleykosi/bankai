"use client";

import { useEffect, useRef } from "react";
import { useRouter } from "next/navigation";

const isModifiedClick = (event: MouseEvent) =>
  event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;

const DEBUG_KEY = "bankai:debug:clicks";

export function LinkClickInterceptor() {
  const router = useRouter();
  const lastNavRef = useRef<string | null>(null);

  useEffect(() => {
    const handleClick = (event: MouseEvent) => {
      if (event.button !== 0 || isModifiedClick(event)) {
        return;
      }

      const target = event.target as Element | null;
      const anchor = target?.closest("a") as HTMLAnchorElement | null;
      if (!anchor || !anchor.href) {
        return;
      }

      if (anchor.target && anchor.target !== "_self") {
        return;
      }
      if (anchor.hasAttribute("download")) {
        return;
      }

      let url: URL;
      try {
        url = new URL(anchor.href, window.location.href);
      } catch {
        return;
      }

      if (url.origin !== window.location.origin) {
        return;
      }

      const href = `${url.pathname}${url.search}${url.hash}`;
      const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
      lastNavRef.current = href;
      const debug = window.localStorage.getItem(DEBUG_KEY) === "1";
      const wasPrevented = event.defaultPrevented;
      event.preventDefault();
      try {
        router.push(href);
        if (debug) {
          console.info("[debug] router.push", href, { defaultPrevented: wasPrevented });
        }
      } catch (error) {
        if (debug) {
          console.error("[debug] router.push failed", error);
        }
      }

      window.setTimeout(() => {
        const now = `${window.location.pathname}${window.location.search}${window.location.hash}`;
        if (now === current) {
          if (debug) {
            console.warn("[debug] router push stalled, hard navigating", {
              from: current,
              to: href,
            });
          }
          window.location.assign(url.toString());
        }
      }, 250);
    };

    window.addEventListener("click", handleClick, true);
    return () => window.removeEventListener("click", handleClick, true);
  }, [router]);

  return null;
}
