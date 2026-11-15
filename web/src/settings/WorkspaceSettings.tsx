// Workspace settings: members (admins), API tokens and the MCP connection box.

import { useCallback, useEffect, useId, useState, type FormEvent } from "react";
import { Link } from "wouter";
import { errorText } from "../api/client";
import { auth, workspaceMembers } from "../api/endpoints";
import { ROLE_ADMIN, ROLE_GUEST, ROLE_MEMBER, type Member, type Token } from "../api/types";
import { useIsAdmin, useSession } from "../app/session";
import { TopBar } from "../app/Shell";
import { Avatar, Empty, ErrorBox, fieldError, generalError, Loading, userName } from "../components/bits";
import { ConfirmDialog } from "../components/Dialog";
import { IconCopy, IconTrash } from "../components/Icons";
import { formatDateTime } from "../lib/dates";

const ROLE_NAME: Record<number, string> = { [ROLE_ADMIN]: "Admin", [ROLE_MEMBER]: "Member", [ROLE_GUEST]: "Guest" };

export function WorkspaceSettings({ tab }: { tab: string }) {
  const isAdmin = useIsAdmin();
  const tabs = [...(isAdmin ? [["members", "Members"]] : []), ["tokens", "API tokens & MCP"]];
  return (
    <>
      <TopBar title="Workspace settings" />
      <nav className="tabs" aria-label="Settings sections">
        {tabs.map(([k, label]) => (
          <Link key={k} href={`/settings/${k}`} aria-current={tab === k ? "page" : undefined}>{label}</Link>
        ))}
      </nav>
      <div className="page">
        <div className="page-pad">
          {tab === "members" && (isAdmin ? <Members /> : <p className="muted">Only admins can see the member list.</p>)}
          {tab === "tokens" && <Tokens />}
          {tab !== "members" && tab !== "tokens" && <Empty title="Unknown settings page" />}
        </div>
      </div>
    </>
  );
}

function Members() {
  const [list, setList] = useState<Member[] | undefined>();
  const [error, setError] = useState("");
  const load = useCallback(() => {
    setError("");
    workspaceMembers().then(setList, (e) => setError(errorText(e)));
  }, []);
  useEffect(load, [load]);
  return (
    <>
      <section className="settings-section">
        <h2>Members</h2>
        {error && <ErrorBox error={error} onRetry={load} />}
        {!list && !error && <Loading />}
        {list && (
          <div className="list-card">
            {list.length === 0 && <div className="list-row muted">No members.</div>}
            {list.map((m) => (
              <div key={m.id} className="list-row">
                <Avatar user={m} />
                <div className="grow">
                  <div>{userName(m)}</div>
                  <div className="muted small">{m.email}</div>
                </div>
                <span className="badge">{ROLE_NAME[m.role] ?? `Role ${m.role}`}</span>
              </div>
            ))}
          </div>
        )}
        <p className="muted small" style={{ marginTop: 8 }}>Changing a role or removing a member is not available yet.</p>
      </section>
      <AddUser onAdded={(m) => setList((l) => [...(l ?? []), m])} />
    </>
  );
}

function AddUser({ onAdded }: { onAdded: (m: Member) => void }) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState(ROLE_MEMBER);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [done, setDone] = useState("");
  const ids = { e: useId(), n: useId(), p: useId(), r: useId() };
  const submit = async (ev: FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setErr(null);
    setDone("");
    try {
      const m = await auth.createUser({ email: email.trim(), display_name: name.trim(), password, role });
      onAdded(m);
      setDone(`Added ${m.email}. Share the password with them privately.`);
      setEmail("");
      setName("");
      setPassword("");
    } catch (x) {
      setErr(x);
    } finally {
      setBusy(false);
    }
  };
  const shown = ["email", "password", "role"];
  return (
    <section className="settings-section">
      <h2>Add user</h2>
      <form className="list-card" style={{ padding: 14 }} onSubmit={submit} autoComplete="off">
        <div className="row-gap" style={{ alignItems: "flex-start" }}>
          <div className="field" style={{ flex: 1, minWidth: 180 }}>
            <label htmlFor={ids.e}>Email</label>
            <input id={ids.e} type="email" className="input" value={email} onChange={(e) => setEmail(e.target.value)} aria-invalid={!!fieldError(err, "email")} />
            {fieldError(err, "email") && <p className="err">{fieldError(err, "email")}</p>}
          </div>
          <div className="field" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor={ids.n}>Display name</label>
            <input id={ids.n} className="input" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        </div>
        <div className="row-gap" style={{ alignItems: "flex-start" }}>
          <div className="field" style={{ flex: 1, minWidth: 180 }}>
            <label htmlFor={ids.p}>Initial password</label>
            <input id={ids.p} type="password" className="input" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)}
              aria-invalid={!!fieldError(err, "password")} aria-describedby={`${ids.p}-h`} />
            <span id={`${ids.p}-h`} className="muted small">At least 8 characters.</span>
            {fieldError(err, "password") && <p className="err">{fieldError(err, "password")}</p>}
          </div>
          <div className="field" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor={ids.r}>Role</label>
            <select id={ids.r} className="input" value={role} onChange={(e) => setRole(Number(e.target.value))}>
              <option value={ROLE_MEMBER}>Member: create and edit work</option>
              <option value={ROLE_GUEST}>Guest: read and comment</option>
              <option value={ROLE_ADMIN}>Admin: everything, including settings</option>
            </select>
            {fieldError(err, "role") && <p className="err">{fieldError(err, "role")}</p>}
          </div>
        </div>
        {generalError(err, shown) && <p className="err" role="alert">{generalError(err, shown)}</p>}
        {done && <p className="ok-text" role="status">{done}</p>}
        <button type="submit" className="btn primary" disabled={busy || !email.trim() || !password}>{busy ? "Adding…" : "Add user"}</button>
      </form>
    </section>
  );
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button type="button" className="btn small" aria-label={label}
      onClick={() => {
        navigator.clipboard?.writeText(text).then(
          () => {
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          },
          () => setCopied(false),
        );
      }}>
      <IconCopy size={14} /> {copied ? "Copied" : "Copy"}
    </button>
  );
}

function Tokens() {
  const { user } = useSession();
  const [list, setList] = useState<Token[] | undefined>();
  const [error, setError] = useState("");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState<unknown>(null);
  const [secret, setSecret] = useState<Token | null>(null);
  const [deleting, setDeleting] = useState<Token | null>(null);
  const id = useId();
  const load = useCallback(() => {
    setError("");
    auth.tokens().then(setList, (e) => setError(errorText(e)));
  }, []);
  useEffect(load, [load]);
  const mcpUrl = `${window.location.origin}/mcp`;

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      const t = await auth.createToken(label.trim());
      setSecret(t);
      setLabel("");
      setList((l) => [{ ...t, token: undefined }, ...(l ?? [])]);
    } catch (x) {
      setFormErr(x);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <section className="settings-section">
        <h2>API tokens</h2>
        <p className="muted">Tokens act as you ({user.email}). Use them with the REST API (<code>X-API-Key</code> header) or an AI agent.</p>
        <form onSubmit={create} className="row-gap" style={{ marginBottom: 10 }}>
          <label htmlFor={id} className="sr-only">Token label</label>
          <input id={id} className="input" style={{ maxWidth: 280 }} placeholder="Label, e.g. laptop agent" value={label} onChange={(e) => setLabel(e.target.value)} />
          <button type="submit" className="btn primary" disabled={busy}>{busy ? "Creating…" : "Create token"}</button>
        </form>
        {generalError(formErr) && <p className="err" role="alert">{generalError(formErr)}</p>}
        {secret?.token && (
          <div className="secret" role="status">
            <div style={{ flex: 1, minWidth: 0 }}>
              <strong>Copy this token now. It will not be shown again.</strong>
              <div><code>{secret.token}</code></div>
            </div>
            <CopyButton text={secret.token} label="Copy token" />
            <button type="button" className="btn small" onClick={() => setSecret(null)}>Done</button>
          </div>
        )}
        {error && <ErrorBox error={error} onRetry={load} />}
        {!list && !error && <Loading />}
        {list && (
          <div className="list-card">
            {list.length === 0 && <div className="list-row muted">No tokens yet.</div>}
            {list.map((t) => (
              <div key={t.id} className="list-row">
                <div className="grow">
                  <div>{t.label || <span className="muted">No label</span>}</div>
                  <div className="muted small">Created {formatDateTime(t.created_at)} · {t.last_used_at ? `last used ${formatDateTime(t.last_used_at)}` : "never used"}</div>
                </div>
                <button type="button" className="icon-btn" aria-label={`Delete token ${t.label || t.id}`} onClick={() => setDeleting(t)}><IconTrash size={14} /></button>
              </div>
            ))}
          </div>
        )}
      </section>
      <section className="settings-section">
        <h2>Connect an AI agent (MCP)</h2>
        <div className="list-card" style={{ padding: 14 }}>
          <p>Point your MCP client at this URL (streamable HTTP):</p>
          <div className="row-gap" style={{ marginBottom: 10 }}>
            <code className="mono" style={{ overflowWrap: "anywhere" }}>{mcpUrl}</code>
            <CopyButton text={mcpUrl} label="Copy MCP URL" />
          </div>
          <p>Send a token from above in the header:</p>
          <code className="mono">Authorization: Bearer &lt;token&gt;</code>
        </div>
      </section>
      {deleting && (
        <ConfirmDialog
          title="Delete token?" confirmLabel="Delete token"
          message={<p>Anything using <strong>{deleting.label || "this token"}</strong> will stop working right away.</p>}
          onClose={() => setDeleting(null)}
          onConfirm={async () => {
            await auth.deleteToken(deleting.id);
            setList((l) => l?.filter((x) => x.id !== deleting.id));
          }}
        />
      )}
    </>
  );
}
