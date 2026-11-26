import { useEffect, useRef, useState } from "react";

/** Quartic ease-out: fast start, long soft landing. Matches --ease-out in styles.css. */
export const easeOut = (t: number) => 1 - (1 - t) ** 4;

export const prefersReducedMotion = () => typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

/**
 * Tweens a flat record of numbers toward `target` on every change. Keys that
 * are new start at their target (callers fade those in with CSS); a change in
 * the middle of a tween continues from the values currently on screen.
 */
export function useTween(target: Record<string, number>, duration = 560): Record<string, number> {
  const [cur, setCur] = useState(target);
  const shown = useRef(target);
  const sig = JSON.stringify(target);
  useEffect(() => {
    const from = shown.current;
    const to = target;
    if (prefersReducedMotion() || duration <= 0) {
      shown.current = to;
      setCur(to);
      return;
    }
    const start = performance.now();
    let raf = 0;
    const tick = (now: number) => {
      const e = easeOut(Math.min(1, (now - start) / duration));
      const next: Record<string, number> = {};
      for (const k in to) next[k] = from[k] == null || !Number.isFinite(from[k]) ? to[k] : from[k] + (to[k] - from[k]) * e;
      shown.current = next;
      setCur(next);
      if (e < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sig, duration]);
  return cur;
}
