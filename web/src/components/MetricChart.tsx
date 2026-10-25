import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { CHROME } from "../lib/palette";
import { formatUnit, type Unit } from "../lib/units";

export interface ChartSeries {
  label: string;
  values: (number | null)[];
  color: string;
}

const axis = (extra: Partial<uPlot.Axis> = {}): uPlot.Axis => ({
  stroke: CHROME.muted, font: CHROME.font, grid: { stroke: CHROME.grid, width: 1 },
  ticks: { stroke: CHROME.axis, width: 1, size: 4 }, ...extra,
});

/**
 * A time-series chart. Null values draw gaps (never zero). Clicking the plot
 * reports the timestamp under the cursor via onPick.
 */
export function MetricChart({ title, xsMs, series, unit, onPick, height = 180, index = 0 }: {
  title: string;
  xsMs: number[];
  series: ChartSeries[];
  unit: Unit;
  onPick?: (tsMs: number) => void;
  height?: number;
  index?: number; // position in a grid, for the staggered entrance
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
        // One series: the title names it. Several: a legend whose values follow
        // the cursor — the hover layer.
        legend: { show: series.length > 1, live: true },
        cursor: { points: { size: 8, width: 2 } },
        scales: unit === "percent" ? { y: { range: [0, 100] } } : {},
        axes: [
          axis(),
          axis({ size: 70, values: (_u, vals) => vals.map((v) => formatUnit(v, unit)) }),
        ],
        series: [{}, ...series.map((s) => ({
          label: s.label, stroke: s.color, width: 2, spanGaps: false,
          fill: series.length === 1 ? s.color + "14" : undefined, // a faint wash under a lone series
          value: (_u: uPlot, v: number | null) => (v === null ? "—" : formatUnit(v, unit)),
        }))],
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
    <figure className="card metric-chart enter" style={{ "--i": index } as React.CSSProperties}>
      <figcaption>{title}</figcaption>
      <div ref={el} className={onPick ? "pickable" : undefined} />
    </figure>
  );
}
