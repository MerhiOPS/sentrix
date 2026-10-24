import { api } from "./client";
import type { components } from "./types.gen";

type S = components["schemas"];
export type Me = S["Me"];
export type Host = S["Host"];
export type HostList = S["HostList"];
export type Sample = S["Sample"];
export type CoreFrame = S["CoreFrame"];
export type EnrollmentToken = S["EnrollmentToken"];

export const login = (email: string, password: string) => api<Me>("POST", "/auth/login", { email, password });
export const logout = () => api<void>("POST", "/auth/logout");
export const me = () => api<Me>("GET", "/me");
export const listHosts = () => api<HostList>("GET", "/hosts");
export const createEnrollmentToken = () => api<EnrollmentToken>("POST", "/enrollment-tokens");

export type Frame = S["Frame"];
export type SeriesList = S["SeriesList"];
export type DetailFrame = S["DetailFrame"];
export type ProcessSnapshot = S["ProcessSnapshot"];

const q = (p: Record<string, string | undefined>) =>
  "?" + new URLSearchParams(Object.entries(p).filter((e): e is [string, string] => e[1] !== undefined)).toString();

export const getHost = (id: string) => api<Host>("GET", `/hosts/${id}`);
export const hostMetrics = (id: string, gauges: string[], from: string, to: string) =>
  api<Frame>("GET", `/hosts/${id}/metrics` + q({ gauges: gauges.join(","), from, to }));
export const listSeries = (id: string) => api<SeriesList>("GET", `/hosts/${id}/series`);
export const seriesMetrics = (id: string, sid: number, from: string, to: string) =>
  api<DetailFrame>("GET", `/hosts/${id}/series/${sid}/metrics` + q({ from, to }));
export const hostProcesses = (id: string) => api<ProcessSnapshot>("GET", `/hosts/${id}/processes`);
