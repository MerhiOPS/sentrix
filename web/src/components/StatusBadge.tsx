import { statusLabel, type HostStatus } from "../lib/status";

export function StatusBadge({ status }: { status: HostStatus }) {
  return <span className={`badge badge-${status}`}>{statusLabel[status]}</span>;
}
