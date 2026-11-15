// Small typed fetch client. Every request carries the CSRF header the server
// requires on cookie-authenticated writes. Errors become ApiError with a
// plain-words message and per-field messages.

import type { Page } from "./types";

export class ApiError extends Error {
  status: number;
  fields: Record<string, string>;
  body: unknown;
  constructor(status: number, message: string, fields: Record<string, string> = {}, body: unknown = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fields = fields;
    this.body = body;
  }
}

const IGNORED_KEYS = new Set(["id", "code", "unsupported_parameters"]);

function humanField(key: string): string {
  if (key === "non_field_errors") return "";
  const s = key.replace(/_/g, " ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function asText(v: unknown): string | null {
  if (typeof v === "string") return v;
  if (Array.isArray(v)) {
    const parts = v.filter((x) => typeof x === "string") as string[];
    return parts.length ? parts.join(" ") : null;
  }
  return null;
}

/** Turns any server error body into a message and field messages. */
export function parseError(status: number, body: unknown): ApiError {
  const fallback =
    status === 0
      ? "Could not reach the server. Check your connection."
      : status === 401
        ? "Please sign in again."
        : status === 403
          ? "You do not have permission to do this."
          : status === 404
            ? "Not found."
            : status >= 500
              ? "The server had a problem. Try again."
              : `Request failed (${status}).`;
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return new ApiError(status, typeof body === "string" && body.trim() && body.length < 200 ? body.trim() : fallback, {}, body);
  }
  const o = body as Record<string, unknown>;
  for (const k of ["error", "detail", "message"]) {
    const t = asText(o[k]);
    if (t) return new ApiError(status, t, {}, body);
  }
  const fields: Record<string, string> = {};
  const lines: string[] = [];
  for (const [k, v] of Object.entries(o)) {
    if (IGNORED_KEYS.has(k)) continue;
    const t = asText(v);
    if (!t) continue;
    fields[k] = t;
    const label = humanField(k);
    lines.push(label ? `${label}: ${t}` : t);
  }
  return new ApiError(status, lines.length ? lines.join(" ") : fallback, fields, body);
}

/** Returns the message for an unknown thrown value. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return String(e);
}

let onUnauthorized: () => void = () => {};

/** Registers what to do on a 401 (return to the login screen). */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  query?: Record<string, string | number | undefined | null>;
  /** Do not trigger the global 401 handler (used by login). */
  allow401?: boolean;
  signal?: AbortSignal;
}

export function buildUrl(path: string, query?: RequestOptions["query"]): string {
  if (!query) return path;
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== null && v !== "") qs.set(k, String(v));
  }
  const s = qs.toString();
  return s ? `${path}${path.includes("?") ? "&" : "?"}${s}` : path;
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", "X-Requested-With": "mostlydone" };
  let body: string | undefined;
  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  let res: Response;
  try {
    res = await fetch(buildUrl(path, opts.query), {
      method: opts.method ?? "GET",
      headers,
      body,
      credentials: "same-origin",
      signal: opts.signal,
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") throw e;
    throw new ApiError(0, "Could not reach the server. Check your connection.");
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!res.ok) {
    if (res.status === 401 && !opts.allow401) onUnauthorized();
    throw parseError(res.status, data);
  }
  return data as T;
}

export const api = {
  get: <T>(path: string, query?: RequestOptions["query"], signal?: AbortSignal) => request<T>(path, { query, signal }),
  post: <T>(path: string, body: unknown = {}) => request<T>(path, { method: "POST", body }),
  patch: <T>(path: string, body: unknown) => request<T>(path, { method: "PATCH", body }),
  del: (path: string) => request<void>(path, { method: "DELETE" }),
};

/** Reads the page index out of a "<per_page>:<page>:<is_prev>" cursor. */
export function cursorPage(cursor: string): number {
  const parts = cursor.split(":");
  if (parts.length !== 3) return -1;
  const n = Number(parts[1]);
  return Number.isInteger(n) ? n : -1;
}

/**
 * Follows next_cursor until the server says there are no more pages, or
 * until maxItems rows are loaded. `fetchPage` gets the cursor ("" first).
 */
export async function fetchAllPages<T>(
  fetchPage: (cursor: string) => Promise<Page<T>>,
  maxItems = 10000,
): Promise<{ items: T[]; total: number; truncated: boolean }> {
  const items: T[] = [];
  let cursor = "";
  let total = 0;
  for (let guard = 0; guard < 1000; guard++) {
    const page = await fetchPage(cursor);
    total = page.total_count;
    items.push(...(page.results ?? []));
    if (!page.next_page_results || !page.results?.length) return { items, total, truncated: false };
    if (items.length >= maxItems) return { items, total, truncated: true };
    cursor = page.next_cursor;
  }
  return { items, total, truncated: true };
}
