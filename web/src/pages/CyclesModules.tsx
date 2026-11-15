// Cycles and modules: lists with dates, status and progress; create, edit, delete.

import { useId, useState, type FormEvent } from "react";
import { Link } from "wouter";
import { refresh, useProjectList } from "../api/cache";
import { cycles as cyclesApi, modules as modulesApi } from "../api/endpoints";
import { MODULE_STATUSES, type Cycle, type Module, type Project } from "../api/types";
import { useCanWrite, useIsAdmin, useProjectByIdent, useSession } from "../app/session";
import { TopBar } from "../app/Shell";
import { Empty, ErrorBox, fieldError, generalError, Loading } from "../components/bits";
import { ConfirmDialog, Dialog } from "../components/Dialog";
import { IconEdit, IconPlus, IconTrash } from "../components/Icons";
import { cycleStatus, dayOf, formatDay, progress } from "../lib/dates";

type Kind = "cycle" | "module";
type Row = Cycle | Module;

const STATUS_LABEL: Record<string, string> = {
  draft: "Draft", upcoming: "Upcoming", current: "Current", completed: "Completed",
  backlog: "Backlog", planned: "Planned", "in-progress": "In progress", paused: "Paused", cancelled: "Cancelled",
};

export function CyclesPage({ ident }: { ident: string }) {
  return <ListPage ident={ident} kind="cycle" />;
}
export function ModulesPage({ ident }: { ident: string }) {
  return <ListPage ident={ident} kind="module" />;
}

function ListPage({ ident, kind }: { ident: string; kind: Kind }) {
  const project = useProjectByIdent(ident);
  const { projects } = useSession();
  if (!project) return projects ? <Empty title={`No project with identifier ${ident}`} /> : <Loading />;
  return <ListBody key={project.id + kind} project={project} kind={kind} />;
}

function ListBody({ project, kind }: { project: Project; kind: Kind }) {
  const list = useProjectList(project.id, kind === "cycle" ? "cycles" : "modules");
  const canWrite = useCanWrite();
  const isAdmin = useIsAdmin();
  const { user } = useSession();
  const [editing, setEditing] = useState<Row | "new" | null>(null);
  const [deleting, setDeleting] = useState<Row | null>(null);
  const noun = kind === "cycle" ? "Cycle" : "Module";
  const enabled = kind === "cycle" ? project.cycle_view : project.module_view;
  const base = `/p/${project.identifier}/${kind === "cycle" ? "cycles" : "modules"}`;

  const rows = (list.data ?? []) as Row[];
  return (
    <>
      <TopBar title={`${project.name} › ${noun}s`}>
        {canWrite && enabled && (
          <button type="button" className="btn primary" onClick={() => setEditing("new")}><IconPlus size={14} /> New {noun.toLowerCase()}</button>
        )}
      </TopBar>
      <div className="page">
        <div className="page-pad">
          {!enabled && <p className="muted">{noun}s are turned off for this project.</p>}
          {list.error && <ErrorBox error={list.error} onRetry={list.reload} />}
          {list.loading && <Loading />}
          {list.data && rows.length === 0 && (
            <Empty title={`No ${noun.toLowerCase()}s yet`}>
              <p>{kind === "cycle" ? "Cycles are time boxes: plan what goes into the next one or two weeks." : "Modules group work items by feature or area."}</p>
            </Empty>
          )}
          {rows.length > 0 && (
            <div className="list-card">
              {rows.map((r) => {
                const status = kind === "cycle" ? cycleStatus((r as Cycle).start_date, (r as Cycle).end_date) : (r as Module).status;
                const start = r.start_date;
                const end = kind === "cycle" ? (r as Cycle).end_date : (r as Module).target_date;
                const pct = progress(r);
                const canDelete = canWrite && (isAdmin || r.created_by === user.id);
                return (
                  <div key={r.id} className="list-row">
                    <div className="grow">
                      <Link href={`${base}/${r.id}`}><strong>{r.name}</strong></Link>
                      {r.description && <div className="muted small">{r.description}</div>}
                    </div>
                    <span className="muted small" style={{ minWidth: 130 }}>
                      {start || end ? `${formatDay(start) || "…"} – ${formatDay(end) || "…"}` : "No dates"}
                    </span>
                    <span className={`badge ${status}`}>{STATUS_LABEL[status] ?? status}</span>
                    <span className="row-gap small" title={`${r.completed_issues} of ${r.total_issues} completed`}>
                      <span className="bar" role="img" aria-label={`${pct}% complete`}><span style={{ width: `${pct}%` }} /></span>
                      {r.completed_issues}/{r.total_issues}
                    </span>
                    {canWrite && (
                      <button type="button" className="icon-btn" aria-label={`Edit ${r.name}`} onClick={() => setEditing(r)}><IconEdit size={14} /></button>
                    )}
                    {canDelete && (
                      <button type="button" className="icon-btn" aria-label={`Delete ${r.name}`} onClick={() => setDeleting(r)}><IconTrash size={14} /></button>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
      {editing && <EditDialog project={project} kind={kind} row={editing === "new" ? null : editing} onClose={() => setEditing(null)} />}
      {deleting && (
        <ConfirmDialog
          title={`Delete ${noun.toLowerCase()}?`}
          message={<p>Delete <strong>{deleting.name}</strong>? Its work items stay in the project.</p>}
          confirmLabel="Delete"
          onClose={() => setDeleting(null)}
          onConfirm={async () => {
            if (kind === "cycle") await cyclesApi.remove(project.id, deleting.id);
            else await modulesApi.remove(project.id, deleting.id);
            await refresh(project.id, kind === "cycle" ? "cycles" : "modules");
          }}
        />
      )}
    </>
  );
}

function EditDialog({ project, kind, row, onClose }: { project: Project; kind: Kind; row: Row | null; onClose: () => void }) {
  const [name, setName] = useState(row?.name ?? "");
  const [description, setDescription] = useState(row?.description ?? "");
  const [start, setStart] = useState(dayOf(row?.start_date) ?? "");
  const [end, setEnd] = useState(dayOf(kind === "cycle" ? (row as Cycle | null)?.end_date : (row as Module | null)?.target_date) ?? "");
  const [status, setStatus] = useState<string>((row as Module | null)?.status ?? "planned");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const ids = { n: useId(), d: useId(), s: useId(), e: useId(), st: useId() };
  const noun = kind === "cycle" ? "cycle" : "module";
  const endKey = kind === "cycle" ? "end_date" : "target_date";

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const common = { name: name.trim(), description: description.trim(), start_date: start || null };
      if (kind === "cycle") {
        const body = { ...common, end_date: end || null };
        if (row) await cyclesApi.update(project.id, row.id, body);
        else await cyclesApi.create(project.id, body);
      } else {
        const body = { ...common, target_date: end || null, status };
        if (row) await modulesApi.update(project.id, row.id, body);
        else await modulesApi.create(project.id, body);
      }
      await refresh(project.id, kind === "cycle" ? "cycles" : "modules");
      onClose();
    } catch (x) {
      setErr(x);
      setBusy(false);
    }
  };
  const shown = ["name", "start_date", endKey, "status"];

  return (
    <Dialog title={row ? `Edit ${noun}` : `New ${noun}`} onClose={onClose}>
      <form onSubmit={submit}>
        <div className="dialog-body">
          <div className="field">
            <label htmlFor={ids.n}>Name</label>
            <input id={ids.n} className="input" value={name} onChange={(e) => setName(e.target.value)} data-autofocus aria-invalid={!!fieldError(err, "name")} />
            {fieldError(err, "name") && <p className="err">{fieldError(err, "name")}</p>}
          </div>
          <div className="field">
            <label htmlFor={ids.d}>Description</label>
            <textarea id={ids.d} className="input" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
          </div>
          <div className="row-gap">
            <div className="field" style={{ flex: 1 }}>
              <label htmlFor={ids.s}>Start date</label>
              <input id={ids.s} type="date" className="input" value={start} onChange={(e) => setStart(e.target.value)} />
              {fieldError(err, "start_date") && <p className="err">{fieldError(err, "start_date")}</p>}
            </div>
            <div className="field" style={{ flex: 1 }}>
              <label htmlFor={ids.e}>{kind === "cycle" ? "End date" : "Target date"}</label>
              <input id={ids.e} type="date" className="input" value={end} onChange={(e) => setEnd(e.target.value)} />
              {fieldError(err, endKey) && <p className="err">{fieldError(err, endKey)}</p>}
            </div>
          </div>
          {kind === "cycle" && <p className="muted small">Set both dates, or leave both empty for a draft cycle.</p>}
          {kind === "module" && (
            <div className="field">
              <label htmlFor={ids.st}>Status</label>
              <select id={ids.st} className="input" value={status} onChange={(e) => setStatus(e.target.value)}>
                {MODULE_STATUSES.map((s) => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
              </select>
            </div>
          )}
          {generalError(err, shown) && <p className="err" role="alert">{generalError(err, shown)}</p>}
        </div>
        <div className="dialog-foot">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={busy || !name.trim()}>{busy ? "Saving…" : row ? "Save" : `Create ${noun}`}</button>
        </div>
      </form>
    </Dialog>
  );
}
