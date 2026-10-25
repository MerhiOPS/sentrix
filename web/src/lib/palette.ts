/**
 * Chart colours. The series slots are the validated dark categorical
 * palette (dataviz reference: lightness band, chroma floor, colour-blind
 * separation and 3:1 contrast against the card surface all pass), used in
 * fixed order — never cycled: a chart past the last slot folds to "other".
 * Status colours are reserved for state and always ship with a label.
 */
export const SERIES = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#008300"] as const;

export const OTHER = "#898781";

/** Slot i, or the muted "other" colour past the validated slots. */
export function seriesColor(i: number): string {
  return SERIES[i] ?? OTHER;
}

export const STATUS = { good: "#0ca30c", warning: "#fab219", serious: "#ec835a", critical: "#d03b3b" } as const;

/** Chart chrome: recessive grid and axes, text in ink tokens. */
export const CHROME = {
  grid: "#2c2c2a",
  axis: "#383835",
  muted: "#898781",
  font: '11px system-ui, -apple-system, "Segoe UI", sans-serif',
} as const;
