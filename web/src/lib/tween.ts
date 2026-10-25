/** Ease-out cubic: fast start, gentle landing. */
export const easeOut = (t: number) => 1 - Math.pow(1 - Math.min(Math.max(t, 0), 1), 3);

/** The value at time t (0..1) of a tween from a to b. */
export function tweenAt(a: number, b: number, t: number): number {
  return a + (b - a) * easeOut(t);
}

/** Whether the user asked the system for less motion. */
export function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
}
