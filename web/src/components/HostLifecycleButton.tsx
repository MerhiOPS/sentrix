import { useMutation, useQueryClient } from "@tanstack/react-query";
import { disableHost, enableHost } from "../api/alerts";

/** Admin: stop a host ingesting and being alerted on, or let it back in. */
export function HostLifecycleButton({ hostId, disabled }: { hostId: string; disabled: boolean }) {
  const qc = useQueryClient();
  const m = useMutation({
    mutationFn: () => (disabled ? enableHost(hostId) : disableHost(hostId)),
    onSuccess: () => qc.invalidateQueries(),
  });
  return (
    <button className={disabled ? "" : "danger"} disabled={m.isPending}
      title={disabled ? "" : "The agent is refused at once and alert rules skip this host"}
      onClick={() => (disabled || confirm("Disable this host? Its agent will be refused.")) && m.mutate()}>
      {disabled ? "Enable host" : "Disable host"}
    </button>
  );
}
