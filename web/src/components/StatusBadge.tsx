import { statusLabel, type HostStatus } from "../lib/status";

/** Status is never colour alone: a dot, a label, and the colour together. */
export function StatusBadge({ status }: { status: HostStatus }) {
  return (
    <span className={`badge badge-${status}`}>
      <i className="dot" aria-hidden />
      {statusLabel[status]}
    </span>
  );
}
