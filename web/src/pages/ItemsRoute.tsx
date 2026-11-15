// Work items page (also used for one cycle or one module): filter bar,
// list/board toggle, and the detail panel when the URL names an item.

import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useSearch } from "wouter";
import { useProjectList } from "../api/cache";
import { errorText } from "../api/client";
import { issues as issuesApi } from "../api/endpoints";
import type { Issue, Project } from "../api/types";
import { useCanWrite, useProjectByIdent, useSession } from "../app/session";
import { TopBar } from "../app/Shell";
import { Empty, ErrorBox, Loading } from "../components/bits";
import { IconBoard, IconList, IconPlus } from "../components/Icons";
import { filtersToParams, filtersToQuery, parseFilters, type Filters, type ListFilterKey } from "../lib/filters";
import { applyIssueEvent, onIssue } from "../lib/issueEvents";
import { prefs, type ViewMode } from "../lib/prefs";
import { DetailPanel, DetailPage } from "../detail/DetailShell";
import { FilterBar } from "./FilterBar";
import { ItemsList } from "./ItemsList";

const ItemsBoard = lazy(() => import("./ItemsBoard"));

interface Props {
  ident: string;
  itemKey?: string;
  cycleId?: string;
  moduleId?: string;
}

export function ItemsRoute({ ident, itemKey, cycleId, moduleId }: Props) {
  const project = useProjectByIdent(ident);
  const { projects, projectsError } = useSession();
  if (!project) {
    if (projectsError) return <ErrorBox error={projectsError} />;
    if (!projects) return <Loading />;
    return (
      <>
        <TopBar title="Project not found" />
        <Empty title={`No project with identifier ${ident}`} />
      </>
    );
  }
  const base = `/p/${project.identifier}/${cycleId ? `cycles/${cycleId}` : moduleId ? `modules/${moduleId}` : "items"}`;
  const panel = !!itemKey && (window.history.state as { panel?: boolean } | null)?.panel === true;
  if (itemKey && !panel) return <DetailPage project={project} itemKey={itemKey} backHref={base} />;
  return (
    <>
      <ItemsPage key={`${project.id}:${cycleId ?? ""}:${moduleId ?? ""}`} project={project} base={base} cycleId={cycleId} moduleId={moduleId} selectedKey={itemKey} />
      {itemKey && <DetailPanel project={project} itemKey={itemKey} base={base} />}
    </>
  );
}

function useScopeTitle(project: Project, cycleId?: string, moduleId?: string): string {
  const cycles = useProjectList(cycleId ? project.id : undefined, "cycles");
  const modules = useProjectList(moduleId ? project.id : undefined, "modules");
  if (cycleId) return `${project.name} › ${cycles.data?.find((c) => c.id === cycleId)?.name ?? "Cycle"}`;
  if (moduleId) return `${project.name} › ${modules.data?.find((m) => m.id === moduleId)?.name ?? "Module"}`;
  return project.name;
}

interface PageProps {
  project: Project;
  base: string;
  cycleId?: string;
  moduleId?: string;
  selectedKey?: string;
}

function ItemsPage({ project, base, cycleId, moduleId, selectedKey }: PageProps) {
  const search = useSearch();
  const [, navigate] = useLocation();
  const { openNewItem } = useSession();
  const canWrite = useCanWrite();
  const [view, setViewState] = useState<ViewMode>(prefs.view());
  const filters = useMemo(() => parseFilters(search), [search]);
  const fixed = useMemo(() => {
    const f: Partial<Record<ListFilterKey, string>> = {};
    if (cycleId) f.cycle = cycleId;
    if (moduleId) f.module = moduleId;
    return f;
  }, [cycleId, moduleId]);
  const params = useMemo(() => filtersToParams(filters, fixed), [filters, fixed]);
  const paramsKey = JSON.stringify(params);
  const title = useScopeTitle(project, cycleId, moduleId);

  const [items, setItems] = useState<Issue[] | undefined>();
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState("");
  const [loadTick, setLoadTick] = useState(0);

  useEffect(() => {
    let live = true;
    setError("");
    issuesApi.list(project.id, JSON.parse(paramsKey) as Record<string, string>).then(
      (r) => {
        if (!live) return;
        setItems(r.items);
        setTruncated(r.truncated);
      },
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [project.id, paramsKey, loadTick]);

  useEffect(
    () =>
      onIssue((e) =>
        setItems((list) =>
          list
            ? applyIssueEvent(list, e, (i) =>
                i.project === project.id && (!cycleId || i.cycle_id === cycleId) && (!moduleId || (i.module_ids ?? []).includes(moduleId)),
              )
            : list,
        ),
      ),
    [project.id, cycleId, moduleId],
  );

  const setFilters = useCallback(
    (f: Filters) => {
      const q = filtersToQuery(f);
      navigate(`${window.location.pathname}${q ? "?" + q : ""}`, { replace: true, state: window.history.state });
    },
    [navigate],
  );

  const setView = (v: ViewMode) => {
    setViewState(v);
    prefs.setView(v);
  };

  const openItem = useCallback(
    // Switching items while the panel is open replaces the entry, so Back returns to the list.
    (key: string) => navigate(`${base}/${key}${search ? "?" + search : ""}`, { replace: !!selectedKey, state: { panel: true } }),
    [base, search, navigate, selectedKey],
  );
  const hrefFor = useCallback((key: string) => `${base}/${key}${search ? "?" + search : ""}`, [base, search]);

  const common = { project, items: items ?? [], setItems, hrefFor, openItem, selectedKey, cycleId, moduleId };

  return (
    <>
      <TopBar title={title}>
        <div className="seg" role="group" aria-label="View">
          <button type="button" aria-pressed={view === "list"} onClick={() => setView("list")}><IconList size={14} /> List</button>
          <button type="button" aria-pressed={view === "board"} onClick={() => setView("board")}><IconBoard size={14} /> Board</button>
        </div>
        {canWrite && (
          <button type="button" className="btn primary" onClick={openNewItem} title="New work item (C)">
            <IconPlus size={14} /> New
          </button>
        )}
      </TopBar>
      <FilterBar project={project} filters={filters} onChange={setFilters} hide={{ cycle: !!cycleId, module: !!moduleId }} />
      <div className="page" style={view === "board" ? { overflow: "hidden" } : undefined}>
        {error ? (
          <div style={{ padding: 16 }}><ErrorBox error={error} onRetry={() => setLoadTick((n) => n + 1)} /></div>
        ) : !items ? (
          <Loading text="Loading work items…" />
        ) : (
          <>
            {truncated && <p className="muted small" style={{ padding: "6px 16px" }}>Showing the first {items.length} work items. Narrow the filters to see the rest.</p>}
            {view === "list" ? (
              <ItemsList {...common} />
            ) : (
              <Suspense fallback={<Loading />}>
                <ItemsBoard {...common} />
              </Suspense>
            )}
          </>
        )}
      </div>
    </>
  );
}

export interface ItemsViewProps {
  project: Project;
  items: Issue[];
  setItems: React.Dispatch<React.SetStateAction<Issue[] | undefined>>;
  hrefFor: (key: string) => string;
  openItem: (key: string) => void;
  selectedKey?: string;
  cycleId?: string;
  moduleId?: string;
}
