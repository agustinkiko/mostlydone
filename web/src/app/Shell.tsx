// The signed-in app: sidebar, routes, global keyboard shortcuts.

import { createContext, lazy, Suspense, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { Redirect, Route, Switch, useLocation } from "wouter";
import { errorText } from "../api/client";
import { projects as projectsApi } from "../api/endpoints";
import type { Project, User } from "../api/types";
import { Empty, Loading, Toasts } from "../components/bits";
import { IconMenu } from "../components/Icons";
import { hasOpenLayer, useLayer } from "../components/layers";
import { CyclesPage, ModulesPage } from "../pages/CyclesModules";
import { ItemsRoute } from "../pages/ItemsRoute";
import { ProjectSettings } from "../settings/ProjectSettings";
import { WorkspaceSettings } from "../settings/WorkspaceSettings";
import { NewItemDialog } from "./NewItemDialog";
import { SessionContext, useSession, type Session } from "./session";
import { Sidebar } from "./Sidebar";

const Palette = lazy(() => import("./Palette"));

const NavContext = createContext<() => void>(() => {});

/** The page header with the mobile menu button. */
export function TopBar({ title, children }: { title: ReactNode; children?: ReactNode }) {
  const toggle = useContext(NavContext);
  return (
    <header className="topbar">
      <button type="button" className="icon-btn menu-btn" aria-label="Open navigation" onClick={toggle}><IconMenu /></button>
      <h1>{title}</h1>
      <span className="spacer" />
      {children}
    </header>
  );
}

function isTyping(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || (el as HTMLElement).isContentEditable;
}

export function Shell({ user, role, slug, onLogout }: { user: User; role: number; slug: string; onLogout: () => void }) {
  const [projects, setProjects] = useState<Project[] | undefined>();
  const [projectsError, setProjectsError] = useState("");
  const [navOpen, setNavOpen] = useState(false);
  const [newItem, setNewItem] = useState(false);
  const [palette, setPalette] = useState(false);
  const [location] = useLocation();

  const reloadProjects = useCallback(async () => {
    try {
      const list = await projectsApi.list();
      setProjects(list);
      setProjectsError("");
      return list;
    } catch (e) {
      setProjectsError(errorText(e));
      return undefined;
    }
  }, []);
  useEffect(() => {
    void reloadProjects();
  }, [reloadProjects]);
  useEffect(() => setNavOpen(false), [location]);
  useLayer(() => setNavOpen(false), navOpen);

  const session: Session = useMemo(
    () => ({
      user, role, slug, projects, projectsError, reloadProjects, logout: onLogout,
      openNewItem: () => setNewItem(true),
      openPalette: () => setPalette(true),
    }),
    [user, role, slug, projects, projectsError, reloadProjects, onLogout],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette(true);
        return;
      }
      if (e.key === "c" && !e.metaKey && !e.ctrlKey && !e.altKey && !isTyping(document.activeElement) && !hasOpenLayer()) {
        e.preventDefault();
        setNewItem(true);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  return (
    <SessionContext.Provider value={session}>
      <NavContext.Provider value={() => setNavOpen((o) => !o)}>
        <div className={`shell${navOpen ? " nav-open" : ""}`}>
          <Sidebar />
          <div className="scrim" onClick={() => setNavOpen(false)} />
          <main className="main" id="main">
            <Routes />
          </main>
        </div>
        {newItem && <NewItemDialog onClose={() => setNewItem(false)} />}
        {palette && (
          <Suspense fallback={null}>
            <Palette onClose={() => setPalette(false)} />
          </Suspense>
        )}
        <Toasts />
      </NavContext.Provider>
    </SessionContext.Provider>
  );
}

function Home() {
  const { projects, projectsError, reloadProjects, role } = useSession();
  if (projectsError) {
    return (
      <>
        <TopBar title="MostlyDone" />
        <Empty title="Could not load projects">
          <p>{projectsError}</p>
          <button type="button" className="btn" onClick={() => void reloadProjects()}>Try again</button>
        </Empty>
      </>
    );
  }
  if (!projects) return <Loading />;
  if (projects.length) return <Redirect to={`/p/${projects[0].identifier}/items`} replace />;
  return (
    <>
      <TopBar title="MostlyDone" />
      <Empty title="No projects yet">
        <p>{role >= 15 ? "Create a project from the sidebar to start tracking work." : "Ask an admin to create a project."}</p>
      </Empty>
    </>
  );
}

function NotFound() {
  return (
    <>
      <TopBar title="Not found" />
      <Empty title="This page does not exist">
        <a href="/">Go to the start page</a>
      </Empty>
    </>
  );
}

function Routes() {
  const { role } = useSession();
  return (
    <Switch>
      <Route path="/"><Home /></Route>
      <Route path="/p/:ident/items">{(p) => <ItemsRoute ident={p.ident} />}</Route>
      <Route path="/p/:ident/items/:key">{(p) => <ItemsRoute ident={p.ident} itemKey={p.key} />}</Route>
      <Route path="/p/:ident/cycles">{(p) => <CyclesPage ident={p.ident} />}</Route>
      <Route path="/p/:ident/cycles/:cid">{(p) => <ItemsRoute ident={p.ident} cycleId={p.cid} />}</Route>
      <Route path="/p/:ident/cycles/:cid/:key">{(p) => <ItemsRoute ident={p.ident} cycleId={p.cid} itemKey={p.key} />}</Route>
      <Route path="/p/:ident/modules">{(p) => <ModulesPage ident={p.ident} />}</Route>
      <Route path="/p/:ident/modules/:mid">{(p) => <ItemsRoute ident={p.ident} moduleId={p.mid} />}</Route>
      <Route path="/p/:ident/modules/:mid/:key">{(p) => <ItemsRoute ident={p.ident} moduleId={p.mid} itemKey={p.key} />}</Route>
      <Route path="/p/:ident/settings">{(p) => <ProjectSettings ident={p.ident} />}</Route>
      <Route path="/p/:ident">{(p) => <Redirect to={`/p/${p.ident}/items`} replace />}</Route>
      <Route path="/settings"><Redirect to={role >= 20 ? "/settings/members" : "/settings/tokens"} replace /></Route>
      <Route path="/settings/:tab">{(p) => <WorkspaceSettings tab={p.tab} />}</Route>
      <Route><NotFound /></Route>
    </Switch>
  );
}
