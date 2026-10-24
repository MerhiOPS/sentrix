import { formatBytes } from "./format";

export type Unit = "percent" | "bytes" | "rate" | "load";

/** Axis/tooltip text for a value in a unit. Null is "no data". */
export function formatUnit(v: number | null | undefined, unit: Unit): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return "—";
  switch (unit) {
    case "percent":
      return `${v.toFixed(0)}%`;
    case "bytes":
      return formatBytes(v);
    case "rate":
      return `${formatBytes(v)}/s`;
    default:
      return v.toFixed(2);
  }
}
