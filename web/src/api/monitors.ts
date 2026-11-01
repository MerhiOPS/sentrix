import { api } from "./client";
import type { components } from "./types.gen";

type S = components["schemas"];
export type Monitor = S["Monitor"];
export type MonitorInput = S["MonitorInput"];
export type MonitorList = S["MonitorList"];
export type MonitorStatus = S["MonitorState"]["status"];
export type CheckResult = S["CheckResult"];
export type IncidentList = S["IncidentList"];
export type DayBar = S["DayBar"];
export type ErrorClass = S["CheckErrorClass"];

export const listMonitors = () => api<MonitorList>("GET", "/monitors");
export const getMonitor = (id: number) => api<Monitor>("GET", `/monitors/${id}`);
export const createMonitor = (m: MonitorInput) => api<Monitor>("POST", "/monitors", m);
export const updateMonitor = (id: number, m: MonitorInput) => api<Monitor>("PUT", `/monitors/${id}`, m);
export const deleteMonitor = (id: number) => api<void>("DELETE", `/monitors/${id}`);
export const pauseMonitor = (id: number) => api<void>("POST", `/monitors/${id}/pause`);
export const resumeMonitor = (id: number) => api<void>("POST", `/monitors/${id}/resume`);
export const checkMonitor = (id: number) => api<void>("POST", `/monitors/${id}/check`);
export const monitorResults = (id: number, from: string, to: string) =>
  api<S["CheckResultList"]>("GET", `/monitors/${id}/results?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`);
export const monitorIncidents = (id: number) => api<IncidentList>("GET", `/monitors/${id}/incidents?limit=50`);
