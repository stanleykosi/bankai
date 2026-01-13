"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

const isModifiedClick = (event: MouseEvent) =>
  event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;

const DEBUG_KEY = "bankai:debug:clicks";
const NAV_FALLBACK_MS = 600;

export function LinkClickInterceptor() {
  const router = useRouter();

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
      if (href === current) {
        return;
      }
      const debug = window.localStorage.getItem(DEBUG_KEY) === "1";
      const wasPrevented = event.defaultPrevented;
      event.preventDefault();
      const beforeHref = window.location.href;
      const beforeState = window.history.state;
      try {
        router.push(href);
        if (debug) {
          console.info("[debug] router.push", href, { defaultPrevented: wasPrevented });
        }
      } catch (error) {
        if (debug) {
          console.error("[debug] router.push failed", error);
        }
        window.location.assign(url.toString());
        return;
      }

      const fallback = () => {
        const nowHref = window.location.href;
        const nowState = window.history.state;
        if (nowHref !== beforeHref || nowState !== beforeState) {
          return;
        }
        if (debug) {
          console.warn("[debug] router.push stalled, forcing reload", {
            from: beforeHref,
            to: url.toString(),
          });
        }
        window.location.assign(url.toString());
      };

      const rafId = window.requestAnimationFrame(() => {
        const nowHref = window.location.href;
        const nowState = window.history.state;
        if (nowHref !== beforeHref || nowState !== beforeState) {
          return;
        }
        window.setTimeout(fallback, NAV_FALLBACK_MS);
      });

      window.setTimeout(() => {
        window.cancelAnimationFrame(rafId);
      }, NAV_FALLBACK_MS + 100);
    };

    window.addEventListener("click", handleClick, true);
    return () => window.removeEventListener("click", handleClick, true);
  }, [router]);

  return null;
}
