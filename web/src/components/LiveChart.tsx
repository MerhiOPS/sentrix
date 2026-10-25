import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { CHROME, seriesColor } from "../lib/palette";
import type { TimeBuffer } from "../lib/ringbuffer";

const SERIES = [
  { key: "cpu", label: "CPU %", stroke: seriesColor(0) },
  { key: "mem", label: "Memory %", stroke: seriesColor(1) },
];

/** CPU and memory over the live window. uPlot redraws only the data. */
export function LiveChart({ buffer, version, width = 320, height = 110 }: {
  buffer: TimeBuffer | undefined;
  version: number;
  width?: number;
  height?: number;
}) {
  const el = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);

  useEffect(() => {
    if (!el.current) return;
    plot.current = new uPlot(
      {
        width, height,
        legend: { show: false },
        scales: { y: { range: [0, 100] } },
        cursor: { show: false },
        axes: [{ show: false }, { size: 36, stroke: CHROME.muted, font: CHROME.font, grid: { stroke: CHROME.grid, width: 1 },
          ticks: { show: false }, splits: () => [0, 50, 100] }],
        series: [{}, ...SERIES.map((s, i) => ({
          label: s.label, stroke: s.stroke, width: 2, spanGaps: false, fill: i === 0 ? s.stroke + "1a" : undefined,
          points: { show: false }, // a sparkline: the line is the data, dots are noise
        }))],
      },
      [[], [], []],
      el.current,
    );
    return () => plot.current?.destroy();
  }, [width, height]);

  useEffect(() => {
    if (buffer && plot.current) {
      plot.current.setData(buffer.columns(SERIES.map((s) => s.key)) as uPlot.AlignedData);
    }
  }, [buffer, version]);

  return (
    <figure className="chart">
      <div ref={el} aria-label="CPU and memory, live" />
      <figcaption>
        {SERIES.map((s) => (
          <span key={s.key} className="legend"><i style={{ background: s.stroke }} />{s.label}</span>
        ))}
      </figcaption>
    </figure>
  );
}
