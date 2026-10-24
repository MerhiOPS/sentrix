import { useHostMetrics } from "../hooks/useHostData";
import type { Unit } from "../lib/units";
import { colorAt, MetricChart } from "./MetricChart";

/** One chart of core metrics (gauges or rates) for the selected range. */
export function CorePanel({ hostId, title, gauges, labels, unit, range, onPick }: {
  hostId: string;
  title: string;
  gauges: string[];
  labels: string[];
  unit: Unit;
  range: string;
  onPick: (tsMs: number) => void;
}) {
  const m = useHostMetrics(hostId, gauges, range);
  if (m.isPending) return <p className="muted">Loading {title}…</p>;
  if (m.isError) return <p className="error">{title}: {m.error.message}</p>;
  const series = gauges.map((g, i) => ({ label: labels[i] ?? g, values: m.data.series[g] ?? [], color: colorAt(i) }));
  return (
    <MetricChart title={`${title} · ${m.data.source} data, ${m.data.step_s}s buckets`} xsMs={m.data.ts_ms}
      series={series} unit={unit} onPick={onPick} />
  );
}
