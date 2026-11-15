// UI preferences in localStorage. Never credentials.

function read<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(`mostlydone:${key}`);
    return v === null ? fallback : (JSON.parse(v) as T);
  } catch {
    return fallback;
  }
}

function write(key: string, v: unknown) {
  try {
    localStorage.setItem(`mostlydone:${key}`, JSON.stringify(v));
  } catch {
    // storage full or blocked: preferences are optional
  }
}

export type ViewMode = "list" | "board";

export const prefs = {
  view: (): ViewMode => (read<string>("view", "list") === "board" ? "board" : "list"),
  setView: (v: ViewMode) => write("view", v),
  collapsed: (pid: string): string[] => {
    const v = read<unknown>(`collapsed:${pid}`, []);
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  },
  setCollapsed: (pid: string, ids: string[]) => write(`collapsed:${pid}`, ids),
};
