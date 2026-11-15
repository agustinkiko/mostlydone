import { useId, useState, type FormEvent } from "react";
import { Link, useLocation } from "wouter";
import { ApiError } from "../api/client";
import { projects as projectsApi } from "../api/endpoints";
import { fieldError, generalError, userName } from "../components/bits";
import { Dialog } from "../components/Dialog";
import {
  IconCycle, IconItems, IconLogout, IconModule, IconPlus, IconSearch, IconSettings, Logo,
} from "../components/Icons";
import { useCanWrite, useSession } from "./session";

export function Sidebar() {
  const { projects, projectsError, user, logout, openPalette } = useSession();
  const canWrite = useCanWrite();
  const [location] = useLocation();
  const [creating, setCreating] = useState(false);
  const current = /^\/p\/([^/]+)/.exec(location)?.[1]?.toUpperCase();

  const nav = (href: string, label: string, icon: React.ReactNode, match = href) => (
    <Link href={href} className={`nav-link nav-sub${location === match || location.startsWith(match + "/") ? " active" : ""}`}
      aria-current={location === href ? "page" : undefined}>
      {icon}
      {label}
    </Link>
  );

  return (
    <nav className="sidebar" aria-label="Main">
      <div className="sidebar-head">
        <span className="brand"><Logo /> Mostlydone</span>
        <span className="spacer" />
        <button type="button" className="icon-btn" aria-label="Search work items (Ctrl+K)" title="Search (Ctrl/⌘ K)" onClick={openPalette}>
          <IconSearch />
        </button>
      </div>
      <div className="sidebar-scroll">
        <div className="sidebar-section">
          <div className="sidebar-title">
            <span>Projects</span>
            {canWrite && (
              <button type="button" className="icon-btn" style={{ width: 22, height: 22 }} aria-label="New project" onClick={() => setCreating(true)}>
                <IconPlus size={14} />
              </button>
            )}
          </div>
          {projectsError && <p className="err">{projectsError}</p>}
          {!projects && !projectsError && <p className="muted small" style={{ padding: "4px 8px" }}>Loading…</p>}
          {projects?.length === 0 && <p className="muted small" style={{ padding: "4px 8px" }}>No projects yet.</p>}
          {projects?.map((p) => {
            const open = p.identifier.toUpperCase() === current;
            const base = `/p/${p.identifier}`;
            return (
              <div key={p.id}>
                <Link href={`${base}/items`} className={`nav-link${open ? " active" : ""}`} aria-expanded={open}>
                  <span className="proj-chip">{p.identifier.slice(0, 4)}</span>
                  <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{p.name}</span>
                </Link>
                {open && (
                  <div>
                    {nav(`${base}/items`, "Work items", <IconItems />)}
                    {p.cycle_view && nav(`${base}/cycles`, "Cycles", <IconCycle />)}
                    {p.module_view && nav(`${base}/modules`, "Modules", <IconModule />)}
                    {nav(`${base}/settings`, "Project settings", <IconSettings />)}
                  </div>
                )}
              </div>
            );
          })}
          {canWrite && (
            <button type="button" className="nav-link" onClick={() => setCreating(true)}>
              <IconPlus /> New project
            </button>
          )}
        </div>
      </div>
      <div className="sidebar-foot">
        <Link href="/settings" className={`nav-link${location.startsWith("/settings") ? " active" : ""}`}>
          <IconSettings /> Workspace settings
        </Link>
        <button type="button" className="nav-link" onClick={logout} title={user.email}>
          <IconLogout /> Log out <span className="muted small" style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{userName(user)}</span>
        </button>
      </div>
      {creating && <NewProjectDialog onClose={() => setCreating(false)} />}
    </nav>
  );
}

/** Suggests an identifier from a name: first letters, upper case, max 5. */
export function suggestIdentifier(name: string): string {
  const words = name.toUpperCase().replace(/[^A-Z0-9 ]/g, " ").split(/\s+/).filter(Boolean);
  if (!words.length) return "";
  const s = words.length > 1 ? words.map((w) => w[0]).join("") : words[0];
  return s.slice(0, 5);
}

function NewProjectDialog({ onClose }: { onClose: () => void }) {
  const { reloadProjects } = useSession();
  const [, navigate] = useLocation();
  const [name, setName] = useState("");
  const [ident, setIdent] = useState("");
  const [identTouched, setIdentTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);
  const ids = { name: useId(), ident: useId(), desc: useId() };
  const identifier = identTouched ? ident : suggestIdentifier(name);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const p = await projectsApi.create({ name: name.trim(), identifier: identifier.trim(), description: description.trim() });
      await reloadProjects();
      onClose();
      navigate(`/p/${p.identifier}/items`);
    } catch (x) {
      setErr(x as ApiError);
      setBusy(false);
    }
  };

  return (
    <Dialog title="New project" onClose={onClose}>
      <form onSubmit={submit}>
        <div className="dialog-body">
          <div className="field">
            <label htmlFor={ids.name}>Name</label>
            <input id={ids.name} className="input" value={name} onChange={(e) => setName(e.target.value)} data-autofocus
              aria-invalid={!!fieldError(err, "name")} />
            {fieldError(err, "name") && <p className="err">{fieldError(err, "name")}</p>}
          </div>
          <div className="field">
            <label htmlFor={ids.ident}>Identifier</label>
            <input id={ids.ident} className="input mono" value={identifier} maxLength={12}
              onChange={(e) => { setIdentTouched(true); setIdent(e.target.value.toUpperCase()); }}
              aria-invalid={!!fieldError(err, "identifier")} aria-describedby={`${ids.ident}-h`} />
            <span id={`${ids.ident}-h`} className="muted small">Used in work item keys, like {identifier || "PROJ"}-12.</span>
            {fieldError(err, "identifier") && <p className="err">{fieldError(err, "identifier")}</p>}
          </div>
          <div className="field">
            <label htmlFor={ids.desc}>Description</label>
            <textarea id={ids.desc} className="input" rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
          </div>
          {generalError(err, ["name", "identifier"]) && <p className="err" role="alert">{generalError(err, ["name", "identifier"])}</p>}
        </div>
        <div className="dialog-foot">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={busy || !name.trim() || !identifier.trim()}>
            {busy ? "Creating…" : "Create project"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
