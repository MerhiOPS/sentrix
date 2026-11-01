import type { MonitorStatus } from "../api/monitors";
import { monitorStatusLabel } from "../lib/uptime";

/** Status is never colour alone: a dot, a label, and the colour together. */
export function MonitorStatusBadge({ status }: { status: MonitorStatus }) {
  return (
    <span className={`badge badge-m-${status}`}>
      <i className="dot" aria-hidden />
      {monitorStatusLabel[status]}
    </span>
  );
}
