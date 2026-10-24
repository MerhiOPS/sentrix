import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { TimeBuffer } from "../lib/ringbuffer";

const SERIES = [
  { key: "cpu", label: "CPU %", stroke: "#4f8cff" },
  { key: "mem", label: "Memory %", stroke: "#f2a33a" },
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
        axes: [{ show: false }, { size: 40, stroke: "#8a93a6", grid: { stroke: "#2a2f3a" } }],
        series: [{}, ...SERIES.map((s) => ({ label: s.label, stroke: s.stroke, width: 1.5, spanGaps: false }))],
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
