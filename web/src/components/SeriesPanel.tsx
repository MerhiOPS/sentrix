import { useSeriesList, useSeriesMetrics } from "../hooks/useHostData";
import type { Unit } from "../lib/units";
import { seriesColor } from "../lib/palette";
import { MetricChart } from "./MetricChart";
import { naturalCompare } from "../lib/naturalSort";

function SeriesChart({ hostId, sid, title, unit, range, color, index }: {
  hostId: string; sid: number; title: string; unit: Unit; range: string; color: string; index: number;
}) {
  const m = useSeriesMetrics(hostId, sid, range);
  if (!m.data) return <div className="card skeleton" style={{ height: 160 }} />;
  return <MetricChart index={index} title={title} xsMs={m.data.ts_ms} unit={unit} height={120}
    series={[{ label: title, values: m.data.values, color }]} />;
}

/** One small chart per detail series of the given metric (per core, …). */
export function SeriesPanel({ hostId, metric, title, unit, range }: {
  hostId: string; metric: string; title: string; unit: Unit; range: string;
}) {
  const list = useSeriesList(hostId);
  const series = (list.data?.series ?? [])
    .filter((s) => s.metric === metric)
    .sort((a, b) => naturalCompare(Object.values(a.labels).join(" "), Object.values(b.labels).join(" ")));
  if (list.isPending) return null;
  if (series.length === 0) return <p className="muted">No {title.toLowerCase()} data yet.</p>;
  return (
    <section>
      <h3>{title}</h3>
      <div className="mini-grid">
        {/* Small multiples: each chart's title names its series, so they share
            one colour rather than cycling a palette across 16 cores. */}
        {series.map((s, i) => (
          <SeriesChart key={s.id} hostId={hostId} sid={s.id} range={range} unit={unit} color={seriesColor(0)} index={i}
            title={Object.values(s.labels).join(" ")} />
        ))}
      </div>
    </section>
  );
}
