import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRule, deleteRule, listRules, searchLogs, type LogFilter } from "../api/endpoints";

/** Paged search, newest first; fetchNextPage follows the opaque cursor. */
export function useLogSearch(filter: LogFilter, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: ["logs", filter],
    queryFn: ({ pageParam }) => searchLogs(filter, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled,
  });
}

export function useRules(enabled: boolean) {
  return useQuery({ queryKey: ["rules"], queryFn: listRules, enabled });
}

export function useRuleMutations() {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ["rules"] });
  return {
    create: useMutation({ mutationFn: createRule, onSuccess: refresh }),
    remove: useMutation({ mutationFn: deleteRule, onSuccess: refresh }),
  };
}
