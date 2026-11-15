// In-memory cache of the small per-project lists (states, labels, members,
// cycles, modules). Components read through useProjectList; writes call
// refresh() so every reader updates.

import { useEffect, useSyncExternalStore } from "react";
import { errorText } from "./client";
import { cycles, labels, modules, projects, states } from "./endpoints";
import type { Cycle, Label, Module, State, User } from "./types";

export interface Kinds {
  states: State[];
  labels: Label[];
  members: User[];
  cycles: Cycle[];
  modules: Module[];
}
export type Kind = keyof Kinds;

const loaders: { [K in Kind]: (pid: string) => Promise<Kinds[K]> } = {
  states: (pid) => states.list(pid).then((l) => [...l].sort((a, b) => a.sequence - b.sequence)),
  labels: (pid) => labels.list(pid).then((l) => [...l].sort((a, b) => a.name.localeCompare(b.name))),
  members: (pid) => projects.members(pid),
  cycles: (pid) => cycles.list(pid),
  modules: (pid) => modules.list(pid),
};

interface Entry<T> {
  data?: T;
  error?: string;
  promise?: Promise<void>;
  version: number;
}

const entries = new Map<string, Entry<unknown>>();
const listeners = new Set<() => void>();
let version = 0;

function emit() {
  version++;
  listeners.forEach((l) => l());
}

function key(pid: string, kind: Kind) {
  return `${pid}:${kind}`;
}

function load<K extends Kind>(pid: string, kind: K): Promise<void> {
  const k = key(pid, kind);
  const e = (entries.get(k) as Entry<Kinds[K]>) ?? { version: 0 };
  const p = loaders[kind](pid).then(
    (data) => {
      entries.set(k, { data, version: e.version + 1 });
      emit();
    },
    (err) => {
      entries.set(k, { data: e.data, error: errorText(err), version: e.version + 1 });
      emit();
    },
  );
  entries.set(k, { ...e, promise: p });
  return p;
}

/** Reloads one list for a project (after a write). */
export function refresh(pid: string, kind: Kind): Promise<void> {
  return load(pid, kind);
}

/** Replaces the cached list locally (optimistic or after a write response). */
export function setCached<K extends Kind>(pid: string, kind: K, data: Kinds[K]) {
  const k = key(pid, kind);
  const e = entries.get(k) ?? { version: 0 };
  entries.set(k, { data, version: e.version + 1 });
  emit();
}

export function getCached<K extends Kind>(pid: string, kind: K): Kinds[K] | undefined {
  return (entries.get(key(pid, kind)) as Entry<Kinds[K]> | undefined)?.data;
}

/** Drops everything (on logout). */
export function clearCache() {
  entries.clear();
  emit();
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export interface ListState<T> {
  data: T | undefined;
  error: string | undefined;
  loading: boolean;
  reload: () => void;
}

/** Reads a cached list, loading it on first use. */
export function useProjectList<K extends Kind>(pid: string | undefined, kind: K): ListState<Kinds[K]> {
  useSyncExternalStore(subscribe, () => version);
  const e = pid ? (entries.get(key(pid, kind)) as Entry<Kinds[K]> | undefined) : undefined;
  useEffect(() => {
    if (pid && !entries.get(key(pid, kind))) {
      void load(pid, kind);
    }
  }, [pid, kind]);
  return {
    data: e?.data,
    error: e?.error,
    loading: !!pid && !e?.data && !e?.error,
    reload: () => {
      if (pid) void load(pid, kind);
    },
  };
}
