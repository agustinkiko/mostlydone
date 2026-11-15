// Project states and labels editors. Server refusals (default state,
// non-empty state, duplicate names) are shown on the row that caused them.

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { refresh, useProjectList } from "../api/cache";
import { errorText } from "../api/client";
import { labels as labelsApi, states as statesApi } from "../api/endpoints";
import { STATE_GROUPS, type Label, type Project, type State, type StateGroup } from "../api/types";
import { ErrorBox, Loading } from "../components/bits";
import { IconTrash, StateIcon } from "../components/Icons";
import { GROUP_LABEL } from "../pages/FilterBar";

function useRowError() {
  const [err, setErr] = useState<{ id: string; msg: string } | null>(null);
  const run = async (id: string, fn: () => Promise<unknown>) => {
    setErr(null);
    try {
      await fn();
      return true;
    } catch (e) {
      setErr({ id, msg: errorText(e) });
      return false;
    }
  };
  return { err, run };
}

/** A text input that saves on blur or Enter when changed. */
function InlineName({ value, label, disabled, onSave }: { value: string; label: string; disabled: boolean; onSave: (v: string) => Promise<boolean> }) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  const commit = async () => {
    const t = v.trim();
    if (!t || t === value) {
      setV(value);
      return;
    }
    if (!(await onSave(t))) setV(value);
  };
  return (
    <input className="input" style={{ maxWidth: 260 }} aria-label={label} value={v} disabled={disabled}
      onChange={(e) => setV(e.target.value)} onBlur={() => void commit()}
      onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()} />
  );
}

export function StatesSection({ project, disabled }: { project: Project; disabled: boolean }) {
  const list = useProjectList(project.id, "states");
  const { err, run } = useRowError();
  const reload = () => refresh(project.id, "states");
  const upd = (s: State, body: Partial<State>) => run(s.id, async () => { await statesApi.update(project.id, s.id, body); await reload(); });

  return (
    <section className="settings-section">
      <h2>States</h2>
      {list.error && <ErrorBox error={list.error} onRetry={list.reload} />}
      {list.loading && <Loading />}
      {list.data && (
        <div className="list-card">
          {list.data.map((s) => (
            <div key={s.id}>
              <div className="list-row">
                <StateIcon group={s.group} color={s.color} />
                <ColorInput label={`Color of ${s.name}`} value={s.color} disabled={disabled} onSave={(color) => void upd(s, { color })} />
                <InlineName value={s.name} label={`Name of state ${s.name}`} disabled={disabled} onSave={(name) => upd(s, { name })} />
                <select className="input" style={{ width: "auto" }} aria-label={`Group of ${s.name}`} value={s.group} disabled={disabled}
                  onChange={(e) => void upd(s, { group: e.target.value as StateGroup })}>
                  {STATE_GROUPS.map((g) => <option key={g} value={g}>{GROUP_LABEL[g]}</option>)}
                </select>
                <span className="spacer" />
                {s.default ? <span className="badge current">Default</span> : !disabled && (
                  <button type="button" className="btn small" onClick={() => void upd(s, { default: true })}>Make default</button>
                )}
                {!disabled && (
                  <button type="button" className="icon-btn" aria-label={`Delete state ${s.name}`}
                    onClick={() => void run(s.id, async () => { await statesApi.remove(project.id, s.id); await reload(); })}>
                    <IconTrash size={14} />
                  </button>
                )}
              </div>
              {err?.id === s.id && <p className="err" role="alert" style={{ padding: "0 12px 8px" }}>{err.msg}</p>}
            </div>
          ))}
          {!disabled && <AddRow kind="state" onAdd={async (name, color, group) => { await statesApi.create(project.id, { name, color, group }); await reload(); }} />}
        </div>
      )}
    </section>
  );
}

export function LabelsSection({ project, disabled }: { project: Project; disabled: boolean }) {
  const list = useProjectList(project.id, "labels");
  const { err, run } = useRowError();
  const reload = () => refresh(project.id, "labels");
  const upd = (l: Label, body: { name?: string; color?: string }) =>
    run(l.id, async () => { await labelsApi.update(project.id, l.id, body); await reload(); });

  return (
    <section className="settings-section">
      <h2>Labels</h2>
      {list.error && <ErrorBox error={list.error} onRetry={list.reload} />}
      {list.loading && <Loading />}
      {list.data && (
        <div className="list-card">
          {list.data.length === 0 && <div className="list-row muted">No labels yet.</div>}
          {list.data.map((l) => (
            <div key={l.id}>
              <div className="list-row">
                <ColorInput label={`Color of ${l.name}`} value={l.color} disabled={disabled} onSave={(color) => void upd(l, { color })} />
                <InlineName value={l.name} label={`Name of label ${l.name}`} disabled={disabled} onSave={(name) => upd(l, { name })} />
                <span className="spacer" />
                {!disabled && (
                  <button type="button" className="icon-btn" aria-label={`Delete label ${l.name}`}
                    onClick={() => void run(l.id, async () => { await labelsApi.remove(project.id, l.id); await reload(); })}>
                    <IconTrash size={14} />
                  </button>
                )}
              </div>
              {err?.id === l.id && <p className="err" role="alert" style={{ padding: "0 12px 8px" }}>{err.msg}</p>}
            </div>
          ))}
          {!disabled && <AddRow kind="label" onAdd={async (name, color) => { await labelsApi.create(project.id, { name, color }); await reload(); }} />}
        </div>
      )}
    </section>
  );
}

function AddRow({ kind, onAdd }: { kind: "state" | "label"; onAdd: (name: string, color: string, group: StateGroup) => Promise<void> }) {
  const [name, setName] = useState("");
  const [color, setColor] = useState(kind === "state" ? "#60646c" : "#4a78c2");
  const [group, setGroup] = useState<StateGroup>("unstarted");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const id = useId();
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setError("");
    try {
      await onAdd(name.trim(), color, group);
      setName("");
    } catch (x) {
      setError(errorText(x));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form onSubmit={submit}>
      <div className="list-row">
        <input type="color" className="swatch-input" aria-label={`New ${kind} color`} value={color} onChange={(e) => setColor(e.target.value)} />
        <label htmlFor={id} className="sr-only">New {kind} name</label>
        <input id={id} className="input" style={{ maxWidth: 260 }} placeholder={`New ${kind} name`} value={name} onChange={(e) => setName(e.target.value)} />
        {kind === "state" && (
          <select className="input" style={{ width: "auto" }} aria-label="New state group" value={group} onChange={(e) => setGroup(e.target.value as StateGroup)}>
            {STATE_GROUPS.map((g) => <option key={g} value={g}>{GROUP_LABEL[g]}</option>)}
          </select>
        )}
        <button type="submit" className="btn small" disabled={busy || !name.trim()}>{busy ? "Adding…" : `Add ${kind}`}</button>
      </div>
      {error && <p className="err" role="alert" style={{ padding: "0 12px 8px" }}>{error}</p>}
    </form>
  );
}

/** A color input that saves once the user stops changing it. */
function ColorInput({ value, label, disabled, onSave }: { value: string; label: string; disabled: boolean; onSave: (v: string) => void }) {
  const [v, setV] = useState(hex(value));
  const latest = useRef({ value, onSave });
  latest.current = { value, onSave };
  useEffect(() => setV(hex(value)), [value]);
  useEffect(() => {
    if (v === hex(latest.current.value)) return;
    const t = setTimeout(() => latest.current.onSave(v), 500);
    return () => clearTimeout(t);
  }, [v]);
  return <input type="color" className="swatch-input" aria-label={label} value={v} disabled={disabled} onChange={(e) => setV(e.target.value)} />;
}

/** <input type=color> needs #rrggbb. */
function hex(c: string): string {
  if (/^#[0-9a-f]{6}$/i.test(c)) return c.toLowerCase();
  if (/^#[0-9a-f]{3}$/i.test(c)) return "#" + [...c.slice(1)].map((x) => x + x).join("").toLowerCase();
  return "#888888";
}
