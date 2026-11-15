// The new-work-item dialog (keyboard `c` or the "New" button).

import { useId, useState, type FormEvent } from "react";
import { useLocation } from "wouter";
import { useProjectList } from "../api/cache";
import { ApiError } from "../api/client";
import { issues as issuesApi } from "../api/endpoints";
import { PRIORITIES, ROLE_MEMBER, type IssueInput, type Priority } from "../api/types";
import { fieldError, generalError, PRIORITY_LABEL, toast } from "../components/bits";
import { Dialog } from "../components/Dialog";
import { emitIssue } from "../lib/issueEvents";
import { issueKey, useSession } from "./session";

/** Plain text to safe paragraphs (the server sanitizes again). */
export function textToHtml(text: string): string {
  const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  const paras = text.trim().split(/\n\s*\n/).filter(Boolean);
  if (!paras.length) return "<p></p>";
  return paras.map((p) => `<p>${p.split("\n").map(esc).join("<br>")}</p>`).join("");
}

export function NewItemDialog({ onClose }: { onClose: () => void }) {
  const { projects, role } = useSession();
  const [location, navigate] = useLocation();
  const m = /^\/p\/([^/]+)(?:\/(cycles|modules)\/([^/]+))?/.exec(location);
  const fromUrl = projects?.find((p) => p.identifier.toUpperCase() === m?.[1]?.toUpperCase());
  const [pid, setPid] = useState(fromUrl?.id ?? projects?.[0]?.id ?? "");
  const project = projects?.find((p) => p.id === pid);
  const states = useProjectList(pid || undefined, "states");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [stateId, setStateId] = useState("");
  const [priority, setPriority] = useState<Priority>("none");
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const ids = { p: useId(), n: useId(), d: useId(), s: useId(), pr: useId(), more: useId() };

  if (role < ROLE_MEMBER) {
    return (
      <Dialog title="New work item" onClose={onClose}>
        <div className="dialog-body"><p>Guests can read and comment, but cannot create work items.</p></div>
      </Dialog>
    );
  }
  if (!projects?.length) {
    return (
      <Dialog title="New work item" onClose={onClose}>
        <div className="dialog-body"><p>Create a project first.</p></div>
      </Dialog>
    );
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!project || !name.trim()) return;
    setBusy(true);
    setErr(null);
    const body: IssueInput & { name: string } = { name: name.trim(), priority, description_html: textToHtml(desc) };
    if (stateId) body.state = stateId;
    if (fromUrl?.id === pid && m?.[2] === "cycles" && m[3]) body.cycle_id = m[3];
    if (fromUrl?.id === pid && m?.[2] === "modules" && m[3]) body.module_ids = [m[3]];
    try {
      const created = await issuesApi.create(project.id, body);
      emitIssue({ type: "saved", issue: created });
      const key = issueKey(project, created.sequence_id);
      toast(`Created ${key}`);
      if (more) {
        setName("");
        setDesc("");
        setBusy(false);
      } else {
        onClose();
        navigate(`/p/${project.identifier}/items/${key}`, { state: { panel: true } });
      }
    } catch (x) {
      setErr(x instanceof ApiError ? x : new ApiError(0, String(x)));
      setBusy(false);
    }
  };

  return (
    <Dialog title="New work item" onClose={onClose} wide>
      <form onSubmit={submit}>
        <div className="dialog-body">
          <div className="row-gap" style={{ marginBottom: 12 }}>
            <label htmlFor={ids.p} className="field-label">Project</label>
            <select id={ids.p} className="input" style={{ width: "auto" }} value={pid} onChange={(e) => { setPid(e.target.value); setStateId(""); }}>
              {projects.map((p) => <option key={p.id} value={p.id}>{p.identifier} · {p.name}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor={ids.n}>Title</label>
            <input id={ids.n} className="input" value={name} onChange={(e) => setName(e.target.value)} data-autofocus aria-invalid={!!fieldError(err, "name")} />
            {fieldError(err, "name") && <p className="err">{fieldError(err, "name")}</p>}
          </div>
          <div className="field">
            <label htmlFor={ids.d}>Description</label>
            <textarea id={ids.d} className="input" rows={4} value={desc} onChange={(e) => setDesc(e.target.value)} />
          </div>
          <div className="row-gap">
            <label htmlFor={ids.s} className="field-label">State</label>
            <select id={ids.s} className="input" style={{ width: "auto" }} value={stateId} onChange={(e) => setStateId(e.target.value)}>
              <option value="">Default</option>
              {(states.data ?? []).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            <label htmlFor={ids.pr} className="field-label">Priority</label>
            <select id={ids.pr} className="input" style={{ width: "auto" }} value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
              {PRIORITIES.map((p) => <option key={p} value={p}>{PRIORITY_LABEL[p]}</option>)}
            </select>
          </div>
          {generalError(err, ["name"]) && <p className="err" role="alert">{generalError(err, ["name"])}</p>}
        </div>
        <div className="dialog-foot">
          <label htmlFor={ids.more} className="row-gap small" style={{ marginRight: "auto" }}>
            <input id={ids.more} type="checkbox" checked={more} onChange={(e) => setMore(e.target.checked)} /> Create another
          </label>
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={busy || !name.trim() || !project}>{busy ? "Creating…" : "Create"}</button>
        </div>
      </form>
    </Dialog>
  );
}
