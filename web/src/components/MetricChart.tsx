import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { formatUnit, type Unit } from "../lib/units";

export interface ChartSeries {
  label: string;
  values: (number | null)[];
  color: string;
}

const PALETTE = ["#4f8cff", "#f2a33a", "#3ccf8e", "#c678dd", "#ef5b5b", "#56b6c2"];
export const colorAt = (i: number) => PALETTE[i % PALETTE.length]!;

/**
 * A time-series chart. Null values draw gaps (never zero). Clicking the plot
 * reports the timestamp under the cursor via onPick.
 */
export function MetricChart({ title, xsMs, series, unit, onPick, height = 180 }: {
  title: string;
  xsMs: number[];
  series: ChartSeries[];
  unit: Unit;
  onPick?: (tsMs: number) => void;
  height?: number;
}) {
  const el = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);
  const pick = useRef(onPick);
  pick.current = onPick;
  const shape = series.map((s) => s.label).join("|");

  useEffect(() => {
    const node = el.current;
    if (!node) return;
    const p = new uPlot(
      {
        width: node.clientWidth || 600,
        height,
        legend: { show: series.length > 1, live: false }, // one series: the title says it
        scales: unit === "percent" ? { y: { range: [0, 100] } } : {},
        axes: [
          { stroke: "#8a93a6", grid: { stroke: "#2a2f3a" } },
          { stroke: "#8a93a6", grid: { stroke: "#2a2f3a" }, size: 70, values: (_u, vals) => vals.map((v) => formatUnit(v, unit)) },
        ],
        series: [{}, ...series.map((s) => ({ label: s.label, stroke: s.color, width: 1.5, spanGaps: false }))],
        hooks: {
          ready: [(u) => u.over.addEventListener("click", () => {
            const idx = u.cursor.idx;
            const x = idx === null || idx === undefined ? undefined : u.data[0][idx];
            if (x !== undefined) pick.current?.(x * 1000);
          })],
        },
      },
      [[]],
      node,
    );
    plot.current = p;
    const ro = new ResizeObserver(() => p.setSize({ width: node.clientWidth, height }));
    ro.observe(node);
    return () => {
      ro.disconnect();
      p.destroy();
    };
  }, [shape, unit, height]); // rebuilt only when the series shape changes; data flows through setData below

  useEffect(() => {
    plot.current?.setData([xsMs.map((x) => x / 1000), ...series.map((s) => s.values)] as uPlot.AlignedData);
  }, [xsMs, series]);

  return (
    <figure className="card metric-chart">
      <figcaption>{title}</figcaption>
      <div ref={el} className={onPick ? "pickable" : undefined} />
    </figure>
  );
}
