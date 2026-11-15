// Tells open lists about work items changed elsewhere (detail panel, dialog).

import type { Issue } from "../api/types";

export type IssueEvent = { type: "saved"; issue: Issue } | { type: "deleted"; id: string };

const listeners = new Set<(e: IssueEvent) => void>();

export function emitIssue(e: IssueEvent) {
  listeners.forEach((l) => l(e));
}

export function onIssue(fn: (e: IssueEvent) => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** Applies an event to a list. New items are appended only if `accept` says so. */
export function applyIssueEvent(list: Issue[], e: IssueEvent, accept: (i: Issue) => boolean): Issue[] {
  if (e.type === "deleted") return list.filter((i) => i.id !== e.id);
  const idx = list.findIndex((i) => i.id === e.issue.id);
  if (idx >= 0) {
    const next = [...list];
    next[idx] = e.issue;
    return next;
  }
  return accept(e.issue) ? [...list, e.issue] : list;
}
