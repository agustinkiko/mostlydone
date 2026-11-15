// Small shared pieces: loading/empty/error states, avatars, chips, toasts.

import { useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import type { Label, Priority, User } from "../api/types";
import { formatDay, isOverdue } from "../lib/dates";
import { IconCalendar, PriorityIcon } from "./Icons";

export function Loading({ text = "Loading…" }: { text?: string }) {
  return (
    <div className="loading" role="status">
      <span className="spinner" aria-hidden="true" />
      {text}
    </div>
  );
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <h2>{title}</h2>
      {children}
    </div>
  );
}

export function ErrorBox({ error, onRetry }: { error: string; onRetry?: () => void }) {
  return (
    <div className="err-box" role="alert">
      {error}
      {onRetry && (
        <>
          {" "}
          <button type="button" className="btn small" onClick={onRetry}>Try again</button>
        </>
      )}
    </div>
  );
}

export function userName(u: User | undefined): string {
  if (!u) return "Unknown user";
  return u.display_name || [u.first_name, u.last_name].filter(Boolean).join(" ") || u.email;
}

export function initials(u: User | undefined): string {
  const n = userName(u).trim().split("@")[0];
  if (!n) return "?";
  const parts = n.split(/[\s._-]+/).filter(Boolean);
  const s = parts.length > 1 ? parts[0][0] + parts[1][0] : n.slice(0, 2);
  return s.toUpperCase();
}

export function Avatar({ user }: { user: User | undefined }) {
  return (
    <span className="avatar" title={userName(user)}>
      {initials(user)}
    </span>
  );
}

export function Avatars({ ids, users, max = 3 }: { ids: string[]; users: Map<string, User>; max?: number }) {
  if (!ids.length) return null;
  const names = ids.map((id) => userName(users.get(id))).join(", ");
  return (
    <span className="avatars" aria-label={`Assignees: ${names}`} title={names}>
      {ids.slice(0, max).map((id) => (
        <Avatar key={id} user={users.get(id)} />
      ))}
      {ids.length > max && <span className="avatar">+{ids.length - max}</span>}
    </span>
  );
}

export const PRIORITY_LABEL: Record<Priority, string> = {
  urgent: "Urgent", high: "High", medium: "Medium", low: "Low", none: "No priority",
};

export function PriorityBadge({ priority }: { priority: Priority }) {
  return (
    <span className={`prio ${priority}`} title={PRIORITY_LABEL[priority]} aria-label={`Priority: ${PRIORITY_LABEL[priority]}`} role="img">
      <PriorityIcon priority={priority} />
    </span>
  );
}

export function LabelChip({ label }: { label: Label }) {
  return (
    <span className="label-chip" title={label.name}>
      <span className="dot" style={{ background: label.color || "var(--text-3)" }} />
      {label.name}
    </span>
  );
}

export function DueDate({ date, group }: { date: string | null; group: string | undefined }) {
  if (!date) return null;
  const late = isOverdue(date, group);
  return (
    <span className={`date-chip${late ? " overdue" : ""}`} title={late ? `Overdue: due ${date}` : `Due ${date}`}>
      <IconCalendar size={13} />
      {formatDay(date)}
      {late && <span className="sr-only"> (overdue)</span>}
    </span>
  );
}

// --- toasts ---
interface Toast {
  id: number;
  text: string;
  kind: "info" | "error";
}
let toasts: Toast[] = [];
let tid = 0;
const toastListeners = new Set<() => void>();
function setToasts(t: Toast[]) {
  toasts = t;
  toastListeners.forEach((l) => l());
}

export function toast(text: string, kind: "info" | "error" = "info") {
  const id = ++tid;
  setToasts([...toasts, { id, text, kind }]);
  setTimeout(() => setToasts(toasts.filter((t) => t.id !== id)), kind === "error" ? 7000 : 3500);
}

export function Toasts() {
  const list = useSyncExternalStore(
    (fn) => {
      toastListeners.add(fn);
      return () => toastListeners.delete(fn);
    },
    () => toasts,
  );
  return (
    <div className="toast-wrap" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} className={`toast ${t.kind}`} role={t.kind === "error" ? "alert" : "status"}>
          <span>{t.text}</span>
          <button type="button" aria-label="Dismiss" onClick={() => setToasts(toasts.filter((x) => x.id !== t.id))}>×</button>
        </div>
      ))}
    </div>
  );
}

/** Debounces a value. */
export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** The error text that is not already shown next to one of `shown` fields. */
export function generalError(err: unknown, shown: string[] = []): string {
  if (!err) return "";
  const e = err as { fields?: Record<string, string>; message?: string };
  const fields = e.fields ?? {};
  const keys = Object.keys(fields);
  if (keys.length && keys.every((k) => shown.includes(k))) return "";
  if (keys.length) return keys.filter((k) => !shown.includes(k)).map((k) => fields[k]).join(" ");
  return e.message ?? String(err);
}

export function fieldError(err: unknown, key: string): string | undefined {
  return (err as { fields?: Record<string, string> } | null)?.fields?.[key];
}
