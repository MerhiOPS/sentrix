import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  checkMonitor, createMonitor, deleteMonitor, getMonitor, listMonitors, monitorIncidents, monitorResults, pauseMonitor,
  resumeMonitor, updateMonitor, type MonitorInput,
} from "../api/monitors";
import { createSilence } from "../api/alerts";

/** Checks run at most every 30 s; polling faster would show nothing new. */
const refresh = 15_000;

export function useMonitors() {
  return useQuery({ queryKey: ["monitors"], queryFn: listMonitors, refetchInterval: refresh });
}

export function useMonitor(id: number) {
  return useQuery({ queryKey: ["monitor", id], queryFn: () => getMonitor(id), refetchInterval: refresh });
}

/** The last 24 h of checks, for the response-time chart. */
export function useMonitorResults(id: number) {
  return useQuery({
    queryKey: ["monitor-results", id],
    queryFn: () => {
      const to = new Date(Date.now() + 60_000);
      return monitorResults(id, new Date(to.getTime() - 24 * 3_600_000 - 60_000).toISOString(), to.toISOString());
    },
    refetchInterval: 30_000,
  });
}

export function useMonitorIncidents(id: number) {
  return useQuery({ queryKey: ["monitor-incidents", id], queryFn: () => monitorIncidents(id), refetchInterval: 30_000 });
}

export function useMonitorMutations() {
  const qc = useQueryClient();
  const changed = () => qc.invalidateQueries({ predicate: (q) => String(q.queryKey[0]).startsWith("monitor") });
  return {
    create: useMutation({ mutationFn: createMonitor, onSuccess: changed }),
    update: useMutation({ mutationFn: ({ id, m }: { id: number; m: MonitorInput }) => updateMonitor(id, m), onSuccess: changed }),
    // A deleted monitor's own query is dropped, not refetched: a refetch
    // would 404 under the page that is about to navigate away.
    remove: useMutation({
      mutationFn: deleteMonitor,
      onSuccess: (_, id) => {
        qc.removeQueries({ queryKey: ["monitor", id] });
        return qc.invalidateQueries({ queryKey: ["monitors"] });
      },
    }),
    pause: useMutation({ mutationFn: pauseMonitor, onSuccess: changed }),
    resume: useMutation({ mutationFn: resumeMonitor, onSuccess: changed }),
    check: useMutation({ mutationFn: checkMonitor, onSuccess: () => setTimeout(changed, 3_000) }),
    mute: useMutation({
      mutationFn: ({ id, hours }: { id: number; hours: number }) => {
        const starts = new Date();
        return createSilence({ monitor_id: id, starts: starts.toISOString(),
          ends: new Date(starts.getTime() + hours * 3_600_000).toISOString(), reason: "muted from the monitor page" });
      },
      onSuccess: () => qc.invalidateQueries({ queryKey: ["silences"] }),
    }),
  };
}
