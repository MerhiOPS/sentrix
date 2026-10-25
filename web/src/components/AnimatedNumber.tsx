import { useEffect, useRef, useState } from "react";
import { prefersReducedMotion, tweenAt } from "../lib/tween";

/** A number that glides to its new value instead of jumping (400 ms). The
 * formatter renders every frame, so units and precision stay correct; with
 * reduced motion it simply swaps. */
export function AnimatedNumber({ value, format }: { value: number | null | undefined; format: (v: number | null | undefined) => string }) {
  const [shown, setShown] = useState(value);
  const from = useRef(value);
  useEffect(() => {
    if (value === null || value === undefined || from.current === null || from.current === undefined || prefersReducedMotion()) {
      from.current = value;
      setShown(value);
      return;
    }
    const a = from.current;
    const start = performance.now();
    let raf = 0;
    const step = (now: number) => {
      const t = (now - start) / 400;
      setShown(tweenAt(a, value, t));
      if (t < 1) raf = requestAnimationFrame(step);
      else from.current = value;
    };
    raf = requestAnimationFrame(step);
    return () => {
      cancelAnimationFrame(raf);
      from.current = value;
    };
  }, [value]);
  return <span className="num">{format(shown)}</span>;
}
