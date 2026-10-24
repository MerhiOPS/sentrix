import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createAlertRule, createChannel, createSilence, deleteAlertRule, deleteChannel, deleteSilence,
  listAlertRules, listAlerts, listChannels, listSilences, updateAlertRule, type AlertRuleInput,
} from "../api/alerts";

/** Alerts refresh on the evaluation cadence: nothing changes faster. */
const evalRefresh = 15_000;

export function useAlerts() {
  return useQuery({ queryKey: ["alerts"], queryFn: listAlerts, refetchInterval: evalRefresh });
}

export function useAlertConfig() {
  return {
    rules: useQuery({ queryKey: ["alert-rules"], queryFn: listAlertRules }),
    channels: useQuery({ queryKey: ["channels"], queryFn: listChannels }),
    silences: useQuery({ queryKey: ["silences"], queryFn: listSilences }),
  };
}

export function useAlertMutations() {
  const qc = useQueryClient();
  const refresh = (key: string) => () => qc.invalidateQueries({ queryKey: [key] });
  return {
    createRule: useMutation({ mutationFn: createAlertRule, onSuccess: refresh("alert-rules") }),
    updateRule: useMutation({
      mutationFn: ({ id, rule }: { id: number; rule: AlertRuleInput }) => updateAlertRule(id, rule),
      onSuccess: refresh("alert-rules"),
    }),
    deleteRule: useMutation({ mutationFn: deleteAlertRule, onSuccess: refresh("alert-rules") }),
    createChannel: useMutation({ mutationFn: createChannel, onSuccess: refresh("channels") }),
    deleteChannel: useMutation({ mutationFn: deleteChannel, onSuccess: refresh("channels") }),
    createSilence: useMutation({ mutationFn: createSilence, onSuccess: refresh("silences") }),
    deleteSilence: useMutation({ mutationFn: deleteSilence, onSuccess: refresh("silences") }),
  };
}
