import { useQuery } from "@tanstack/react-query";
import { listHosts } from "../api/endpoints";

/** Host list with latest values. Live frames update charts in between; the
 * poll refreshes status and health. */
export function useHosts() {
  return useQuery({ queryKey: ["hosts"], queryFn: listHosts, refetchInterval: 15_000 });
}
