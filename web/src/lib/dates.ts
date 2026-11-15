// Date helpers. API dates are "YYYY-MM-DD"; cycle dates come as
// "YYYY-MM-DDT00:00:00.000000Z". All comparisons use the calendar day only,
// so there is no time-zone shift.

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** The calendar day part of a date or datetime string, or null. */
export function dayOf(v: string | null | undefined): string | null {
  if (!v) return null;
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(v);
  return m ? `${m[1]}-${m[2]}-${m[3]}` : null;
}

/** Today's local date as YYYY-MM-DD. */
export function todayStr(now: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}

/** "Nov 14" in the current year, "Nov 14, 2025" otherwise. */
export function formatDay(v: string | null | undefined, today: string = todayStr()): string {
  const d = dayOf(v);
  if (!d) return "";
  const [y, m, dd] = d.split("-");
  const base = `${MONTHS[Number(m) - 1]} ${Number(dd)}`;
  return y === today.slice(0, 4) ? base : `${base}, ${y}`;
}

/** Overdue: target day is before today and the item is not done. */
export function isOverdue(target: string | null | undefined, stateGroup: string | undefined, today: string = todayStr()): boolean {
  const d = dayOf(target);
  if (!d) return false;
  if (stateGroup === "completed" || stateGroup === "cancelled") return false;
  return d < today;
}

export type CycleStatus = "draft" | "upcoming" | "current" | "completed";

export function cycleStatus(start: string | null, end: string | null, today: string = todayStr()): CycleStatus {
  const s = dayOf(start);
  const e = dayOf(end);
  if (!s || !e) return "draft";
  if (s > today) return "upcoming";
  if (e < today) return "completed";
  return "current";
}

/** A datetime for humans: "Nov 14, 09:30" (local time). */
export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${MONTHS[t.getMonth()]} ${t.getDate()}, ${t.getFullYear()} ${p(t.getHours())}:${p(t.getMinutes())}`;
}

/** Percent done, 0..100, from the count fields. */
export function progress(c: { total_issues: number; completed_issues: number }): number {
  if (!c.total_issues) return 0;
  return Math.round((c.completed_issues / c.total_issues) * 100);
}
