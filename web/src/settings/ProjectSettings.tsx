import { useId, useState, type FormEvent } from "react";
import { useLocation } from "wouter";
import { clearCache } from "../api/cache";
import { projects as projectsApi } from "../api/endpoints";
import type { Project } from "../api/types";
import { useIsAdmin, useProjectByIdent, useSession } from "../app/session";
import { TopBar } from "../app/Shell";
import { Empty, fieldError, generalError, Loading } from "../components/bits";
import { ConfirmDialog } from "../components/Dialog";
import { LabelsSection, StatesSection } from "./StatesLabels";

export function ProjectSettings({ ident }: { ident: string }) {
  const project = useProjectByIdent(ident);
  const { projects } = useSession();
  if (!project) return projects ? <Empty title={`No project with identifier ${ident}`} /> : <Loading />;
  return <Body key={project.id} project={project} />;
}

function Body({ project }: { project: Project }) {
  const isAdmin = useIsAdmin();
  return (
    <>
      <TopBar title={`${project.name} › Settings`} />
      <div className="page">
        <div className="page-pad">
          {!isAdmin && <p className="muted">Only workspace admins can change project settings. You can view them here.</p>}
          <General project={project} disabled={!isAdmin} />
          <StatesSection project={project} disabled={!isAdmin} />
          <LabelsSection project={project} disabled={!isAdmin} />
          {isAdmin && <Danger project={project} />}
        </div>
      </div>
    </>
  );
}

function General({ project, disabled }: { project: Project; disabled: boolean }) {
  const { reloadProjects } = useSession();
  const [, navigate] = useLocation();
  const [name, setName] = useState(project.name);
  const [identifier, setIdentifier] = useState(project.identifier);
  const [description, setDescription] = useState(project.description);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  const ids = { n: useId(), i: useId(), d: useId() };
  const dirty = name !== project.name || identifier !== project.identifier || description !== project.description;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    setSaved(false);
    try {
      const body: Partial<Project> = {};
      if (name !== project.name) body.name = name.trim();
      if (identifier !== project.identifier) body.identifier = identifier.trim();
      if (description !== project.description) body.description = description;
      const p = await projectsApi.update(project.id, body);
      await reloadProjects();
      setSaved(true);
      if (p.identifier !== project.identifier) navigate(`/p/${p.identifier}/settings`, { replace: true });
    } catch (x) {
      setErr(x);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="settings-section" aria-labelledby={ids.n + "h"}>
      <h2 id={ids.n + "h"}>General</h2>
      <form onSubmit={submit} className="list-card" style={{ padding: 14 }}>
        <div className="row-gap" style={{ alignItems: "flex-start" }}>
          <div className="field" style={{ flex: 2, minWidth: 200 }}>
            <label htmlFor={ids.n}>Name</label>
            <input id={ids.n} className="input" value={name} disabled={disabled} onChange={(e) => setName(e.target.value)} aria-invalid={!!fieldError(err, "name")} />
            {fieldError(err, "name") && <p className="err">{fieldError(err, "name")}</p>}
          </div>
          <div className="field" style={{ flex: 1, minWidth: 120 }}>
            <label htmlFor={ids.i}>Identifier</label>
            <input id={ids.i} className="input mono" value={identifier} maxLength={12} disabled={disabled}
              onChange={(e) => setIdentifier(e.target.value.toUpperCase())} aria-invalid={!!fieldError(err, "identifier")} />
            {fieldError(err, "identifier") && <p className="err">{fieldError(err, "identifier")}</p>}
          </div>
        </div>
        <div className="field">
          <label htmlFor={ids.d}>Description</label>
          <textarea id={ids.d} className="input" rows={3} value={description} disabled={disabled} onChange={(e) => setDescription(e.target.value)} />
        </div>
        {generalError(err, ["name", "identifier"]) && <p className="err" role="alert">{generalError(err, ["name", "identifier"])}</p>}
        {!disabled && (
          <div className="row-gap">
            <button type="submit" className="btn primary" disabled={busy || !dirty || !name.trim() || !identifier.trim()}>{busy ? "Saving…" : "Save changes"}</button>
            {saved && !dirty && <span className="ok-text" role="status">Saved</span>}
          </div>
        )}
      </form>
    </section>
  );
}

function Danger({ project }: { project: Project }) {
  const { reloadProjects } = useSession();
  const [, navigate] = useLocation();
  const [open, setOpen] = useState(false);
  return (
    <section className="settings-section">
      <h2>Delete project</h2>
      <div className="list-card" style={{ padding: 14 }}>
        <p>Deleting a project moves it, with all its work items, cycles, modules, comments and history, to the trash for 30 days. A workspace admin can restore it from workspace settings until then; after that it is deleted permanently.</p>
        <button type="button" className="btn danger" onClick={() => setOpen(true)}>Delete this project</button>
      </div>
      {open && (
        <ConfirmDialog
          title="Move project to trash?" confirmLabel="Move to trash" typeToConfirm={project.name}
          message={<p><strong>{project.name}</strong> ({project.identifier}) and everything in it moves to the trash for 30 days and can be restored by an admin. After that it is deleted permanently.</p>}
          onClose={() => setOpen(false)}
          onConfirm={async () => {
            await projectsApi.remove(project.id);
            clearCache();
            await reloadProjects();
            navigate("/", { replace: true });
          }}
        />
      )}
    </section>
  );
}
