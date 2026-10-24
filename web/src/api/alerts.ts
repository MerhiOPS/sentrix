import { api } from "./client";
import type { components } from "./types.gen";

type S = components["schemas"];
export type AlertRule = S["AlertRule"];
export type AlertRuleInput = S["AlertRuleInput"];
export type Channel = S["Channel"];
export type ChannelInput = S["ChannelInput"];
export type Silence = S["Silence"];
export type SilenceInput = S["SilenceInput"];
export type Alerts = S["Alerts"];
export type AlertEvent = S["AlertEvent"];

export const listAlerts = () => api<Alerts>("GET", "/alerts?limit=100");
export const listAlertRules = () => api<S["AlertRuleList"]>("GET", "/alert-rules");
export const createAlertRule = (r: AlertRuleInput) => api<AlertRule>("POST", "/alert-rules", r);
export const updateAlertRule = (id: number, r: AlertRuleInput) => api<AlertRule>("PUT", `/alert-rules/${id}`, r);
export const deleteAlertRule = (id: number) => api<void>("DELETE", `/alert-rules/${id}`);
export const listChannels = () => api<S["ChannelList"]>("GET", "/channels");
export const createChannel = (c: ChannelInput) => api<Channel>("POST", "/channels", c);
export const deleteChannel = (id: number) => api<void>("DELETE", `/channels/${id}`);
export const listSilences = () => api<S["SilenceList"]>("GET", "/silences");
export const createSilence = (s: SilenceInput) => api<Silence>("POST", "/silences", s);
export const deleteSilence = (id: number) => api<void>("DELETE", `/silences/${id}`);
export const disableHost = (id: string) => api<void>("POST", `/hosts/${id}/disable`);
export const enableHost = (id: string) => api<void>("POST", `/hosts/${id}/enable`);
