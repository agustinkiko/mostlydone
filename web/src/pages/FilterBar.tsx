import { useEffect, useRef, useState } from "react";
import { useProjectList } from "../api/cache";
import { PRIORITIES, STATE_GROUPS, type Project } from "../api/types";
import { PRIORITY_LABEL, userName } from "../components/bits";
import { PriorityIcon, StateIcon } from "../components/Icons";
import { Picker, type Option } from "../components/Picker";
import { activeFilterCount, emptyFilters, type Filters, type ListFilterKey } from "../lib/filters";

export const GROUP_LABEL: Record<string, string> = {
  backlog: "Backlog", unstarted: "Unstarted", started: "Started", completed: "Completed", cancelled: "Cancelled",
};

interface Props {
  project: Project;
  filters: Filters;
  onChange: (f: Filters) => void;
  hide: { cycle?: boolean; module?: boolean };
}

export function FilterBar({ project, filters, onChange, hide }: Props) {
  const members = useProjectList(project.id, "members");
  const labels = useProjectList(project.id, "labels");
  const cycles = useProjectList(project.cycle_view && !hide.cycle ? project.id : undefined, "cycles");
  const modules = useProjectList(project.module_view && !hide.module ? project.id : undefined, "modules");
  const [text, setText] = useState(filters.search);
  const latest = useRef(filters);
  latest.current = filters;
  const sent = useRef(filters.search);

  // Follow the URL when it changes from outside (back button, clear), not
  // when it echoes what this box just sent.
  useEffect(() => {
    if (filters.search !== sent.current) {
      sent.current = filters.search;
      setText(filters.search);
    }
  }, [filters.search]);
  useEffect(() => {
    if (text.trim() === latest.current.search) return;
    const t = setTimeout(() => {
      sent.current = text.trim();
      onChange({ ...latest.current, search: text });
    }, 300);
    return () => clearTimeout(t);
  }, [text, onChange]);

  const set = (k: ListFilterKey, v: string[]) => onChange({ ...filters, [k]: v });

  const chip = (k: ListFilterKey, label: string, options: Option[]) => {
    const sel = filters[k];
    const names = sel.map((v) => options.find((o) => o.value === v)?.label ?? "…");
    return (
      <Picker
        key={k} multi options={options} selected={sel} onChange={(v) => set(k, v)} buttonClass={`chip-btn${sel.length ? " on" : ""}`}
        label={`Filter by ${label.toLowerCase()}`}
      >
        {label}
        {sel.length > 0 && `: ${names.length > 2 ? `${names.length} selected` : names.join(", ")}`}
      </Picker>
    );
  };

  return (
    <div className="filterbar" role="search">
      <input
        className="input search" type="search" placeholder="Search work items" aria-label="Search work items"
        value={text} onChange={(e) => setText(e.target.value)}
      />
      {chip(
        "state_group", "State",
        STATE_GROUPS.map((g) => ({ value: g, label: GROUP_LABEL[g], icon: <StateIcon group={g} color={undefined} /> })),
      )}
      {chip(
        "priority", "Priority",
        PRIORITIES.map((p) => ({ value: p, label: PRIORITY_LABEL[p], icon: <span className={`prio ${p}`}><PriorityIcon priority={p} /></span> })),
      )}
      {chip("assignee", "Assignee", (members.data ?? []).map((u) => ({ value: u.id, label: userName(u) })))}
      {chip(
        "label", "Label",
        (labels.data ?? []).map((l) => ({ value: l.id, label: l.name, icon: <span className="dot" style={{ background: l.color || "var(--text-3)" }} /> })),
      )}
      {!hide.cycle && project.cycle_view && chip("cycle", "Cycle", (cycles.data ?? []).map((c) => ({ value: c.id, label: c.name })))}
      {!hide.module && project.module_view && chip("module", "Module", (modules.data ?? []).map((m) => ({ value: m.id, label: m.name })))}
      {activeFilterCount(filters) > 0 && (
        <button type="button" className="btn ghost small" onClick={() => onChange(emptyFilters())}>Clear filters</button>
      )}
    </div>
  );
}
