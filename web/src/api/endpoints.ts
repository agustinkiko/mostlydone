// One function per REST call the UI makes. Paths match internal/api.

import { api, fetchAllPages, request } from "./client";
import type {
  AccountUser, Activity, Comment, Cycle, Issue, IssueInput, IssueLink, Label, Me, Member, Module, Page, Project,
  SearchHit, State, Token, TrashedProject, User,
} from "./types";

let slug = "main";
export function setWorkspaceSlug(s: string) {
  slug = s;
}
const ws = () => `/api/v1/workspaces/${encodeURIComponent(slug)}`;
const pj = (pid: string) => `${ws()}/projects/${pid}`;
const items = (pid: string) => `${pj(pid)}/work-items/`;
const item = (pid: string, id: string) => `${pj(pid)}/work-items/${id}/`;

function all<T>(path: string, query: Record<string, string> = {}, perPage = 250) {
  return fetchAllPages<T>((cursor) => api.get<Page<T>>(path, { ...query, per_page: perPage, cursor })).then((r) => r.items);
}

// --- auth ---
export const auth = {
  me: () => request<Me>("/auth/me", { allow401: true }),
  login: (email: string, password: string) =>
    request<{ user: User }>("/auth/login", { method: "POST", body: { email, password }, allow401: true }),
  setup: (email: string, password: string, display_name: string, setup_token: string) =>
    request<{ user: User }>("/auth/setup", { method: "POST", body: { email, password, display_name, setup_token }, allow401: true }),
  logout: () => request<void>("/auth/logout", { method: "POST", body: {}, allow401: true }),
  tokens: () => api.get<Token[]>("/auth/tokens"),
  createToken: (label: string) => api.post<Token>("/auth/tokens", { label }),
  deleteToken: (id: string) => api.del(`/auth/tokens/${id}`),
  createUser: (body: { email: string; password: string; display_name: string; role: number }) =>
    api.post<Member>("/auth/users", body),
  users: () => api.get<AccountUser[]>("/auth/users"),
  updateUser: (id: string, body: { role?: number; is_active?: boolean }) =>
    api.patch<Member & { is_active: boolean }>(`/auth/users/${id}`, body),
  changePassword: (current_password: string, new_password: string) =>
    request<void>("/auth/password", { method: "POST", body: { current_password, new_password } }),
};

export const workspaceMembers = () => api.get<Member[]>(`${ws()}/members/`);

// --- projects ---
export const projects = {
  list: () => all<Project>(`${ws()}/projects/`, { order_by: "name" }),
  create: (body: { name: string; identifier: string; description?: string }) => api.post<Project>(`${ws()}/projects/`, body),
  update: (pid: string, body: Partial<Pick<Project, "name" | "identifier" | "description">>) => api.patch<Project>(`${pj(pid)}/`, body),
  remove: (pid: string) => api.del(`${pj(pid)}/`),
  members: (pid: string) => api.get<User[]>(`${pj(pid)}/members/`),
  // Trash (admin only): remove() above moves a project here for 30 days.
  trashed: () => all<TrashedProject>(`${ws()}/trashed-projects/`),
  restore: (id: string) => api.post<Project>(`${ws()}/trashed-projects/${id}/restore/`, {}),
  purge: (id: string) => api.del(`${ws()}/trashed-projects/${id}/`),
};

// --- states and labels ---
export const states = {
  list: (pid: string) => all<State>(`${pj(pid)}/states/`),
  create: (pid: string, body: Partial<State>) => api.post<State>(`${pj(pid)}/states/`, body),
  update: (pid: string, id: string, body: Partial<State>) => api.patch<State>(`${pj(pid)}/states/${id}/`, body),
  remove: (pid: string, id: string) => api.del(`${pj(pid)}/states/${id}/`),
};

export const labels = {
  list: (pid: string) => all<Label>(`${pj(pid)}/labels/`),
  create: (pid: string, body: { name: string; color?: string }) => api.post<Label>(`${pj(pid)}/labels/`, body),
  update: (pid: string, id: string, body: { name?: string; color?: string }) => api.patch<Label>(`${pj(pid)}/labels/${id}/`, body),
  remove: (pid: string, id: string) => api.del(`${pj(pid)}/labels/${id}/`),
};

// --- work items ---
export const issues = {
  /** All items matching the filter params, in board order. */
  list: (pid: string, params: Record<string, string>, maxItems = 5000) =>
    fetchAllPages<Issue>(
      (cursor) => api.get<Page<Issue>>(items(pid), { order_by: "sort_order", ...params, per_page: 250, cursor }),
      maxItems,
    ),
  get: (pid: string, id: string) => api.get<Issue>(item(pid, id)),
  byKey: (key: string) => api.get<Issue>(`${ws()}/work-items/${encodeURIComponent(key)}/`),
  create: (pid: string, body: IssueInput & { name: string }) => api.post<Issue>(items(pid), body),
  update: (pid: string, id: string, body: IssueInput) => api.patch<Issue>(item(pid, id), body),
  remove: (pid: string, id: string) => api.del(item(pid, id)),
  search: (q: string, signal?: AbortSignal) =>
    api.get<{ issues: SearchHit[] }>(`${ws()}/work-items/search/`, { search: q, workspace_search: "true", limit: 20 }, signal),
  searchInProject: (pid: string, q: string) =>
    api.get<{ issues: SearchHit[] }>(`${ws()}/work-items/search/`, { search: q, project_id: pid, limit: 20 }),
};

export const comments = {
  list: (pid: string, iid: string) => all<Comment>(`${item(pid, iid)}comments/`),
  create: (pid: string, iid: string, html: string) => api.post<Comment>(`${item(pid, iid)}comments/`, { comment_html: html }),
  update: (pid: string, iid: string, id: string, html: string) =>
    api.patch<Comment>(`${item(pid, iid)}comments/${id}/`, { comment_html: html }),
  remove: (pid: string, iid: string, id: string) => api.del(`${item(pid, iid)}comments/${id}/`),
};

export const links = {
  list: (pid: string, iid: string) => all<IssueLink>(`${item(pid, iid)}links/`),
  create: (pid: string, iid: string, url: string, title: string) =>
    api.post<IssueLink>(`${item(pid, iid)}links/`, title ? { url, title } : { url }),
  remove: (pid: string, iid: string, id: string) => api.del(`${item(pid, iid)}links/${id}/`),
};

export const activities = {
  list: (pid: string, iid: string) => all<Activity>(`${item(pid, iid)}activities/`, { order_by: "created_at" }),
};

// --- cycles and modules ---
export interface CycleInput {
  name?: string;
  description?: string;
  start_date?: string | null;
  end_date?: string | null;
}
export const cycles = {
  list: (pid: string) => all<Cycle>(`${pj(pid)}/cycles/`),
  get: (pid: string, id: string) => api.get<Cycle>(`${pj(pid)}/cycles/${id}/`),
  create: (pid: string, body: CycleInput) => api.post<Cycle>(`${pj(pid)}/cycles/`, body),
  update: (pid: string, id: string, body: CycleInput) => api.patch<Cycle>(`${pj(pid)}/cycles/${id}/`, body),
  remove: (pid: string, id: string) => api.del(`${pj(pid)}/cycles/${id}/`),
};

export interface ModuleInput {
  name?: string;
  description?: string;
  start_date?: string | null;
  target_date?: string | null;
  status?: string;
}
export const modules = {
  list: (pid: string) => all<Module>(`${pj(pid)}/modules/`),
  get: (pid: string, id: string) => api.get<Module>(`${pj(pid)}/modules/${id}/`),
  create: (pid: string, body: ModuleInput) => api.post<Module>(`${pj(pid)}/modules/`, body),
  update: (pid: string, id: string, body: ModuleInput) => api.patch<Module>(`${pj(pid)}/modules/${id}/`, body),
  remove: (pid: string, id: string) => api.del(`${pj(pid)}/modules/${id}/`),
};
