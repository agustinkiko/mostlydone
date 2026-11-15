// Boot: GET /auth/me decides between first-run setup, login and the app.

import { useCallback, useEffect, useId, useState, type FormEvent } from "react";
import { ApiError, errorText, setUnauthorizedHandler } from "../api/client";
import { clearCache } from "../api/cache";
import { auth, setWorkspaceSlug } from "../api/endpoints";
import type { Me } from "../api/types";
import { ErrorBox, Loading } from "../components/bits";
import { Logo } from "../components/Icons";
import { Shell } from "./Shell";

type Mode = { kind: "loading" } | { kind: "error"; error: string } | { kind: "setup" } | { kind: "login"; notice?: string } | { kind: "app"; me: Me };

export function Boot() {
  const [mode, setMode] = useState<Mode>({ kind: "loading" });

  const boot = useCallback(async () => {
    setMode({ kind: "loading" });
    try {
      const me = await auth.me();
      setWorkspaceSlug(me.workspace_slug);
      if (me.setup_required) setMode({ kind: "setup" });
      else if (!me.user) setMode({ kind: "login" });
      else setMode({ kind: "app", me });
    } catch (e) {
      setMode({ kind: "error", error: errorText(e) });
    }
  }, []);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      clearCache();
      setMode({ kind: "login", notice: "Your session ended. Please sign in again." });
    });
    void boot();
  }, [boot]);

  const logout = useCallback(async () => {
    try {
      await auth.logout();
    } catch {
      // the cookie is cleared server-side when possible; show login either way
    }
    clearCache();
    setMode({ kind: "login" });
  }, []);

  switch (mode.kind) {
    case "loading":
      return <div className="center-page"><Loading /></div>;
    case "error":
      return (
        <div className="center-page">
          <div className="auth-card"><ErrorBox error={mode.error} onRetry={boot} /></div>
        </div>
      );
    case "setup":
      return <AuthForm setup onDone={boot} />;
    case "login":
      return <AuthForm notice={mode.notice} onDone={boot} />;
    case "app":
      if (!mode.me.user) return null;
      return <Shell user={mode.me.user} role={mode.me.role ?? 0} slug={mode.me.workspace_slug} onLogout={logout} />;
  }
}

function AuthForm({ setup, notice, onDone }: { setup?: boolean; notice?: string; onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const ids = { email: useId(), name: useId(), pw: useId() };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (setup) await auth.setup(email.trim(), password, name.trim());
      else await auth.login(email.trim(), password);
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, errorText(err)));
      setBusy(false);
    }
  };
  const fieldErr = (k: string) => error?.fields[k];
  const general = error && !Object.keys(error.fields).some((k) => ["email", "password", "display_name"].includes(k)) ? error.message : "";

  return (
    <div className="center-page">
      <form className="auth-card" onSubmit={submit} noValidate>
        <div className="brand" style={{ marginBottom: 16 }}><Logo /> MostlyDone</div>
        <h1>{setup ? "Set up your workspace" : "Sign in"}</h1>
        <p className="muted">{setup ? "Create the first admin account." : "Use the email and password your admin gave you."}</p>
        {notice && <p className="muted" role="status">{notice}</p>}
        <div className="field">
          <label htmlFor={ids.email}>Email</label>
          <input id={ids.email} className="input" type="email" autoComplete="username" required autoFocus value={email}
            onChange={(e) => setEmail(e.target.value)} aria-invalid={!!fieldErr("email")} />
          {fieldErr("email") && <p className="err">{fieldErr("email")}</p>}
        </div>
        {setup && (
          <div className="field">
            <label htmlFor={ids.name}>Display name</label>
            <input id={ids.name} className="input" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)}
              aria-invalid={!!fieldErr("display_name")} />
            {fieldErr("display_name") && <p className="err">{fieldErr("display_name")}</p>}
          </div>
        )}
        <div className="field">
          <label htmlFor={ids.pw}>Password</label>
          <input id={ids.pw} className="input" type="password" autoComplete={setup ? "new-password" : "current-password"} required
            value={password} onChange={(e) => setPassword(e.target.value)} aria-invalid={!!fieldErr("password")} />
          {fieldErr("password") && <p className="err">{fieldErr("password")}</p>}
        </div>
        {general && <p className="err" role="alert">{general}</p>}
        <button type="submit" className="btn primary" style={{ width: "100%", justifyContent: "center" }} disabled={busy || !email || !password}>
          {busy ? "Please wait…" : setup ? "Create admin and continue" : "Sign in"}
        </button>
      </form>
    </div>
  );
}
