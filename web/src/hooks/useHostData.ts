import { useQuery } from "@tanstack/react-query";
import { getHost, hostMetrics, hostProcesses, listSeries, seriesMetrics } from "../api/endpoints";
import { window } from "../lib/timeRange";

const REFRESH_MS = 30_000;

export function useHost(id: string) {
  return useQuery({ queryKey: ["host", id], queryFn: () => getHost(id), refetchInterval: REFRESH_MS });
}

export function useHostMetrics(id: string, gauges: string[], range: string) {
  return useQuery({
    queryKey: ["metrics", id, gauges.join(","), range],
    queryFn: () => {
      const w = window(range, Date.now());
      return hostMetrics(id, gauges, w.from, w.to);
    },
    refetchInterval: REFRESH_MS,
  });
}

export function useSeriesList(id: string) {
  return useQuery({ queryKey: ["series", id], queryFn: () => listSeries(id), staleTime: 60_000 });
}

export function useSeriesMetrics(id: string, sid: number, range: string) {
  return useQuery({
    queryKey: ["series-metrics", id, sid, range],
    queryFn: () => {
      const w = window(range, Date.now());
      return seriesMetrics(id, sid, w.from, w.to);
    },
    refetchInterval: REFRESH_MS,
  });
}

export function useProcesses(id: string) {
  return useQuery({ queryKey: ["processes", id], queryFn: () => hostProcesses(id), refetchInterval: REFRESH_MS, retry: false });
}
