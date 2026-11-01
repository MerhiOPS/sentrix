import type { DayBar } from "../api/monitors";
import { barLabel, barLevel } from "../lib/uptime";

/**
 * One bar per UTC day, oldest first. Colour is the day's level, and every
 * bar carries the same fact as text (tooltip and accessible name), so the
 * strip is never colour alone. A day without monitored time is a gap.
 */
export function UptimeBars({ bars, legend = false }: { bars: DayBar[]; legend?: boolean }) {
  const down = bars.filter((b) => b.down_s > 0).length;
  return (
    <div className="uptime-bars-wrap">
      <ol className="uptime-bars" aria-label={`Last ${bars.length} days: ${down === 0 ? "no downtime" : `downtime on ${down} day${down === 1 ? "" : "s"}`}`}>
        {bars.map((b) => {
          const text = barLabel(b);
          return <li key={b.day} className={`bar bar-${barLevel(b)}`} title={text} aria-label={text} />;
        })}
      </ol>
      {legend && (
        <p className="bar-key muted small">
          <span><i className="bar bar-good" aria-hidden /> 100 %</span>
          <span><i className="bar bar-warning" aria-hidden /> 99 % or more</span>
          <span><i className="bar bar-critical" aria-hidden /> below 99 %</span>
          <span><i className="bar bar-gap" aria-hidden /> not monitored</span>
        </p>
      )}
    </div>
  );
}
