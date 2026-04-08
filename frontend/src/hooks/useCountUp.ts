import { useEffect, useRef, useState } from 'react';

/**
 * Animates a number from 0 (or its previous value) to `target` over `duration` ms.
 * Uses requestAnimationFrame with an ease-out cubic curve.
 *
 * @param target  – the value to animate toward
 * @param duration – total animation time in ms (default 1200)
 * @param delay    – ms to wait before starting the animation (default 0)
 */
export function useCountUp(target: number, duration = 1200, delay = 0): number {
  const [display, setDisplay] = useState(0);
  const prevTarget = useRef(0);
  const rafId = useRef(0);
  const timeoutId = useRef(0);

  useEffect(() => {
    const from = prevTarget.current;
    const delta = target - from;
    if (delta === 0) return;

    function startAnimation() {
      const start = performance.now();

      function tick(now: number) {
        const elapsed = now - start;
        const progress = Math.min(elapsed / duration, 1);
        // ease-out cubic
        const eased = 1 - Math.pow(1 - progress, 3);
        const current = from + delta * eased;

        setDisplay(current);

        if (progress < 1) {
          rafId.current = requestAnimationFrame(tick);
        } else {
          prevTarget.current = target;
        }
      }

      rafId.current = requestAnimationFrame(tick);
    }

    if (delay > 0) {
      timeoutId.current = window.setTimeout(startAnimation, delay);
    } else {
      startAnimation();
    }

    return () => {
      cancelAnimationFrame(rafId.current);
      window.clearTimeout(timeoutId.current);
    };
  }, [target, duration, delay]);

  // On first mount, snap to 0 then animate
  useEffect(() => {
    prevTarget.current = 0;
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return display;
}
