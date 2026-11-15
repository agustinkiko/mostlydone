// Work item filters <-> URL query <-> REST filter params.

export const LIST_FILTER_KEYS = ["state_group", "priority", "assignee", "label", "cycle", "module"] as const;
export type ListFilterKey = (typeof LIST_FILTER_KEYS)[number];

export interface Filters {
  search: string;
  state_group: string[];
  priority: string[];
  assignee: string[];
  label: string[];
  cycle: string[];
  module: string[];
}

export function emptyFilters(): Filters {
  return { search: "", state_group: [], priority: [], assignee: [], label: [], cycle: [], module: [] };
}

function splitList(v: string | null): string[] {
  if (!v) return [];
  const seen = new Set<string>();
  for (const p of v.split(",")) {
    const t = p.trim();
    if (t) seen.add(t);
  }
  return [...seen];
}

/** Reads filters from a query string ("?a=b" or "a=b"). Unknown keys are ignored. */
export function parseFilters(query: string): Filters {
  const q = new URLSearchParams(query.startsWith("?") ? query.slice(1) : query);
  const f = emptyFilters();
  f.search = (q.get("search") ?? q.get("q") ?? "").trim();
  for (const k of LIST_FILTER_KEYS) f[k] = splitList(q.get(k));
  return f;
}

/** Writes filters to a query string without the leading "?" (stable key order). */
export function filtersToQuery(f: Filters): string {
  const q = new URLSearchParams();
  if (f.search.trim()) q.set("search", f.search.trim());
  for (const k of LIST_FILTER_KEYS) if (f[k].length) q.set(k, f[k].join(","));
  return q.toString();
}

/**
 * REST params for GET .../work-items/. `fixed` (e.g. the cycle of a cycle
 * page) overrides what the URL says for that key.
 */
export function filtersToParams(f: Filters, fixed: Partial<Record<ListFilterKey, string>> = {}): Record<string, string> {
  const out: Record<string, string> = {};
  if (f.search.trim()) out.search = f.search.trim();
  for (const k of LIST_FILTER_KEYS) {
    const v = fixed[k] ?? f[k].join(",");
    if (v) out[k] = v;
  }
  return out;
}

export function activeFilterCount(f: Filters): number {
  return LIST_FILTER_KEYS.reduce((n, k) => n + (f[k].length ? 1 : 0), f.search.trim() ? 1 : 0);
}

export function toggleValue(list: string[], v: string): string[] {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}
