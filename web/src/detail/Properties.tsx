import { useEffect, useId, useState, type ReactNode } from "react";
import { getCached, setCached, useProjectList } from "../api/cache";
import { errorText } from "../api/client";
import { issues as issuesApi, labels as labelsApi } from "../api/endpoints";
import { PRIORITIES, type Issue, type IssueInput, type Project, type SearchHit } from "../api/types";
import { useIsAdmin } from "../app/session";
import { Avatar, LabelChip, PRIORITY_LABEL, useDebounced, userName } from "../components/bits";
import { Dialog } from "../components/Dialog";
import { PriorityIcon, StateIcon } from "../components/Icons";
import { Picker } from "../components/Picker";
import { useLookups } from "../pages/itemBits";

interface Props {
  project: Project;
  issue: Issue;
  save: (patch: IssueInput) => Promise<void>;
  disabled: boolean;
}

const LABEL_COLORS = ["#d9534f", "#e8833a", "#d4a72c", "#3f9e5a", "#2f8f9d", "#4a78c2", "#7a5cc7", "#b34fa0", "#6b7280"];

export function Properties({ project, issue, save, disabled }: Props) {
  const lk = useLookups(project.id);
  const cycles = useProjectList(project.cycle_view ? project.id : undefined, "cycles");
  const modules = useProjectList(project.module_view ? project.id : undefined, "modules");
  const isAdmin = useIsAdmin();
  const [err, setErr] = useState<{ k: string; msg: string } | null>(null);
  const [parentOpen, setParentOpen] = useState(false);

  const run = async (k: string, patch: IssueInput) => {
    setErr(null);
    try {
      await save(patch);
    } catch (e) {
      setErr({ k, msg: errorText(e) });
    }
  };
  const errFor = (k: string) => (err?.k === k ? <p className="err" role="alert" style={{ gridColumn: "1 / -1" }}>{err.msg}</p> : null);

  const st = lk.stateById.get(issue.state);
  const assignees = issue.assignees ?? [];
  const labelIds = issue.labels ?? [];
  const moduleIds = issue.module_ids ?? [];
  const none = <span className="muted">None</span>;

  const row = (k: string, label: string, control: ReactNode) => (
    <div className="prop" key={k}>
      <span className="prop-label">{label}</span>
      <div>{control}</div>
      {errFor(k)}
    </div>
  );

  const createLabel = async (name: string) => {
    const color = LABEL_COLORS[Math.floor(Math.random() * LABEL_COLORS.length)];
    const l = await labelsApi.create(project.id, { name, color });
    setCached(project.id, "labels", [...(getCached(project.id, "labels") ?? []), l].sort((a, b) => a.name.localeCompare(b.name)));
    await run("labels", { labels: [...labelIds, l.id] });
  };

  return (
    <div>
      {row("state", "State",
        <Picker label="State" disabled={disabled} selected={[issue.state]}
          options={lk.states.map((s) => ({ value: s.id, label: s.name, icon: <StateIcon group={s.group} color={s.color} /> }))}
          onChange={(v) => v[0] && v[0] !== issue.state && void run("state", { state: v[0] })}>
          <StateIcon group={st?.group} color={st?.color} /> {st?.name ?? "…"}
        </Picker>)}
      {row("priority", "Priority",
        <Picker label="Priority" disabled={disabled} selected={[issue.priority]}
          options={PRIORITIES.map((p) => ({ value: p, label: PRIORITY_LABEL[p], icon: <span className={`prio ${p}`}><PriorityIcon priority={p} /></span> }))}
          onChange={(v) => v[0] && void run("priority", { priority: v[0] as Issue["priority"] })}>
          <span className={`prio ${issue.priority}`}><PriorityIcon priority={issue.priority} /></span> {PRIORITY_LABEL[issue.priority]}
        </Picker>)}
      {row("assignees", "Assignees",
        <Picker label="Assignees" multi disabled={disabled} selected={assignees}
          options={[...lk.userById.values()].map((u) => ({ value: u.id, label: userName(u), icon: <Avatar user={u} /> }))}
          onChange={(v) => void run("assignees", { assignees: v })}>
          {assignees.length ? assignees.map((id) => <span key={id} className="row-gap" style={{ gap: 4 }}><Avatar user={lk.userById.get(id)} />{userName(lk.userById.get(id))}</span>) : none}
        </Picker>)}
      {row("labels", "Labels",
        <Picker label="Labels" multi disabled={disabled} selected={labelIds} placeholder={isAdmin ? "Filter or create…" : "Filter…"}
          options={[...lk.labelById.values()].map((l) => ({ value: l.id, label: l.name, icon: <span className="dot" style={{ background: l.color || "var(--text-3)" }} /> }))}
          onCreate={isAdmin ? createLabel : undefined}
          onChange={(v) => void run("labels", { labels: v })}>
          {labelIds.length ? labelIds.map((id) => { const l = lk.labelById.get(id); return l ? <LabelChip key={id} label={l} /> : null; }) : none}
        </Picker>)}
      {row("start_date", "Start date", <DateInput label="Start date" value={issue.start_date} disabled={disabled} onChange={(v) => void run("start_date", { start_date: v })} />)}
      {row("target_date", "Target date", <DateInput label="Target date" value={issue.target_date} disabled={disabled} onChange={(v) => void run("target_date", { target_date: v })} />)}
      {project.cycle_view && row("cycle", "Cycle",
        <Picker label="Cycle" disabled={disabled} selected={issue.cycle_id ? [issue.cycle_id] : []}
          options={(cycles.data ?? []).map((c) => ({ value: c.id, label: c.name }))}
          onChange={(v) => void run("cycle", { cycle_id: v[0] ?? null })}>
          {issue.cycle_id ? (cycles.data?.find((c) => c.id === issue.cycle_id)?.name ?? "…") : none}
        </Picker>)}
      {project.module_view && row("modules", "Modules",
        <Picker label="Modules" multi disabled={disabled} selected={moduleIds}
          options={(modules.data ?? []).map((m) => ({ value: m.id, label: m.name }))}
          onChange={(v) => void run("modules", { module_ids: v })}>
          {moduleIds.length ? moduleIds.map((id) => modules.data?.find((m) => m.id === id)?.name ?? "…").join(", ") : none}
        </Picker>)}
      {row("parent", "Parent",
        <button type="button" className="prop-btn" disabled={disabled} onClick={() => setParentOpen(true)} aria-label="Parent work item">
          {issue.parent ? <ParentName id={issue.parent} project={project} /> : none}
        </button>)}
      {parentOpen && (
        <ParentDialog project={project} issue={issue} onClose={() => setParentOpen(false)}
          onPick={async (id) => { await run("parent", { parent: id }); setParentOpen(false); }} />
      )}
    </div>
  );
}

function DateInput({ label, value, disabled, onChange }: { label: string; value: string | null; disabled: boolean; onChange: (v: string | null) => void }) {
  return (
    <input type="date" className="input" aria-label={label} value={value ?? ""} disabled={disabled}
      onChange={(e) => {
        const v = e.target.value || null;
        if (v !== value) onChange(v);
      }} />
  );
}

function ParentName({ id, project }: { id: string; project: Project }) {
  const [text, setText] = useState("…");
  useEffect(() => {
    let live = true;
    issuesApi.get(project.id, id).then(
      (p) => live && setText(`${project.identifier}-${p.sequence_id} ${p.name}`),
      () => live && setText("Unknown item"),
    );
    return () => {
      live = false;
    };
  }, [id, project.id, project.identifier]);
  return <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{text}</span>;
}

function ParentDialog({ project, issue, onClose, onPick }: { project: Project; issue: Issue; onClose: () => void; onPick: (id: string | null) => Promise<void> }) {
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [error, setError] = useState("");
  const dq = useDebounced(q, 200).trim();
  const id = useId();
  useEffect(() => {
    if (!dq) {
      setHits(null);
      return;
    }
    let live = true;
    setError("");
    issuesApi.searchInProject(project.id, dq).then(
      (r) => live && setHits(r.issues),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [project.id, dq]);
  return (
    <Dialog title="Set parent work item" onClose={onClose}>
      <div className="dialog-body">
        <label htmlFor={id} className="field-label">Search in {project.name}</label>
        <input id={id} className="input" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Title or number" data-autofocus />
        {error && <p className="err" role="alert">{error}</p>}
        <ul className="opt-list" style={{ maxHeight: 260 }}>
          {(hits ?? []).filter((h) => h.id !== issue.id).map((h) => (
            <li key={h.id}>
              <button type="button" className="opt" onClick={() => void onPick(h.id)}>
                <span className="muted mono small">{h.project__identifier}-{h.sequence_id}</span> {h.name}
              </button>
            </li>
          ))}
          {hits && !hits.filter((h) => h.id !== issue.id).length && <li className="opt-empty">No matches</li>}
        </ul>
      </div>
      <div className="dialog-foot">
        {issue.parent && <button type="button" className="btn" onClick={() => void onPick(null)}>Remove parent</button>}
        <button type="button" className="btn" onClick={onClose}>Cancel</button>
      </div>
    </Dialog>
  );
}
