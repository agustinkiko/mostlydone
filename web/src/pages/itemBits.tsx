// Pieces shared by the list rows and board cards.

import { useMemo, useRef, useState, type FormEvent } from "react";
import { useProjectList } from "../api/cache";
import { errorText } from "../api/client";
import { issues as issuesApi } from "../api/endpoints";
import type { Issue, IssueInput, Label, Project, State, User } from "../api/types";
import { Avatars, DueDate, LabelChip, PriorityBadge } from "../components/bits";
import { IconPlus, IconSub } from "../components/Icons";
import { emitIssue } from "../lib/issueEvents";

export interface Lookups {
  states: State[];
  stateById: Map<string, State>;
  labelById: Map<string, Label>;
  userById: Map<string, User>;
  loading: boolean;
  error: string | undefined;
}

export function useLookups(pid: string): Lookups {
  const states = useProjectList(pid, "states");
  const labels = useProjectList(pid, "labels");
  const members = useProjectList(pid, "members");
  return useMemo(
    () => ({
      states: states.data ?? [],
      stateById: new Map((states.data ?? []).map((s) => [s.id, s])),
      labelById: new Map((labels.data ?? []).map((l) => [l.id, l])),
      userById: new Map((members.data ?? []).map((u) => [u.id, u])),
      loading: states.loading,
      error: states.error,
    }),
    [states.data, labels.data, members.data, states.loading, states.error],
  );
}

export function ItemMeta({ issue, lk, compact }: { issue: Issue; lk: Lookups; compact?: boolean }) {
  const st = lk.stateById.get(issue.state);
  const labels = (issue.labels ?? []).map((id) => lk.labelById.get(id)).filter((l): l is Label => !!l);
  const max = compact ? 3 : 2;
  return (
    <>
      {issue.priority !== "none" && <PriorityBadge priority={issue.priority} />}
      {labels.slice(0, max).map((l) => (
        <span key={l.id} className="hide-sm"><LabelChip label={l} /></span>
      ))}
      {labels.length > max && <span className="meta-chip hide-sm" title={labels.slice(max).map((l) => l.name).join(", ")}>+{labels.length - max}</span>}
      <DueDate date={issue.target_date} group={st?.group} />
      {issue.sub_issues_count > 0 && (
        <span className="meta-chip" title={`${issue.sub_issues_count} sub-items`} aria-label={`${issue.sub_issues_count} sub-items`}>
          <IconSub size={13} />
          {issue.sub_issues_count}
        </span>
      )}
      <Avatars ids={issue.assignees ?? []} users={lk.userById} />
    </>
  );
}

/** "+ Add work item" that turns into a one-line input. */
export function QuickAdd({ project, defaults, className = "quick-add" }: { project: Project; defaults: IssueInput; className?: string }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    if (!n) return;
    setBusy(true);
    setError("");
    try {
      const created = await issuesApi.create(project.id, { ...defaults, name: n });
      emitIssue({ type: "saved", issue: created });
      setName("");
      input.current?.focus();
    } catch (x) {
      setError(errorText(x));
    } finally {
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <div className={className}>
        <button type="button" className="add-link" onClick={() => setOpen(true)}>
          <IconPlus size={14} /> Add work item
        </button>
      </div>
    );
  }
  return (
    <form className={className} onSubmit={submit} style={{ flexWrap: "wrap" }}>
      <input
        ref={input} className="input" autoFocus placeholder="Work item title, then Enter" aria-label="New work item title"
        value={name} disabled={busy} onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            setOpen(false);
          }
        }}
        onBlur={() => !name.trim() && setOpen(false)}
      />
      {error && <p className="err" role="alert" style={{ width: "100%" }}>{error}</p>}
    </form>
  );
}

/** Handles a click on an item link: plain clicks open the panel in-app. */
export function onItemClick(e: React.MouseEvent, open: () => void) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  open();
}
