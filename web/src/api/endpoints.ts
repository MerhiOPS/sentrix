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
