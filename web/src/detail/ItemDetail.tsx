import { useCallback, useEffect, useRef, useState } from "react";
import { errorText } from "../api/client";
import { refresh } from "../api/cache";
import { issues as issuesApi } from "../api/endpoints";
import type { Issue, IssueInput, Project } from "../api/types";
import { useCanWrite, useIsAdmin, useSession } from "../app/session";
import { Empty, ErrorBox, Loading } from "../components/bits";
import { ConfirmDialog } from "../components/Dialog";
import { IconTrash } from "../components/Icons";
import { emitIssue } from "../lib/issueEvents";
import { Editor, SafeHtml } from "./Editor";
import { Feed } from "./Feed";
import { Properties } from "./Properties";
import { Links, SubItems } from "./Relations";

interface Props {
  project: Project;
  itemKey: string;
  onDeleted: () => void;
}

export function ItemDetail({ project, itemKey, onDeleted }: Props) {
  const [issue, setIssue] = useState<Issue | undefined>();
  const [error, setError] = useState("");
  const [notFound, setNotFound] = useState(false);
  const [feedTick, setFeedTick] = useState(0);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const canWrite = useCanWrite();
  const isAdmin = useIsAdmin();
  const { user } = useSession();
  const current = useRef<Issue | undefined>(undefined);
  current.current = issue;

  const load = useCallback(() => {
    setError("");
    setNotFound(false);
    issuesApi.byKey(itemKey).then(
      (i) => (i.project === project.id ? setIssue(i) : setNotFound(true)),
      (e) => ((e as { status?: number }).status === 404 ? setNotFound(true) : setError(errorText(e))),
    );
  }, [itemKey, project.id]);
  useEffect(load, [load]);

  /** PATCH with optimistic local update. Throws so the caller can show the error in place. */
  const save = useCallback(
    async (patch: IssueInput) => {
      const before = current.current;
      if (!before) return;
      setIssue({ ...before, ...patch } as Issue);
      try {
        const saved = await issuesApi.update(project.id, before.id, patch);
        setIssue(saved);
        emitIssue({ type: "saved", issue: saved });
        setFeedTick((n) => n + 1);
        if ("cycle_id" in patch) void refresh(project.id, "cycles");
        if ("module_ids" in patch) void refresh(project.id, "modules");
      } catch (e) {
        setIssue((cur) => (cur && cur.id === before.id ? before : cur));
        throw e;
      }
    },
    [project.id],
  );

  if (notFound) return <Empty title={`Work item ${itemKey} was not found`}><p>It may have been deleted.</p></Empty>;
  if (error) return <div style={{ padding: 16 }}><ErrorBox error={error} onRetry={load} /></div>;
  if (!issue) return <Loading />;

  const canDelete = canWrite && (isAdmin || issue.created_by === user.id);

  return (
    <div className="detail">
      <div className="detail-main">
        <TitleEditor issue={issue} disabled={!canWrite} onSave={(name) => save({ name })} />
        <div className="section" style={{ marginTop: 4 }}>
          {canWrite ? (
            <Editor key={issue.id} label="Description" initialHtml={issue.description_html} onSave={(html) => save({ description_html: html })} />
          ) : (
            <SafeHtml html={issue.description_html} empty="No description." />
          )}
        </div>
        <SubItems project={project} issue={issue} />
        <Links project={project} issue={issue} onChange={() => setFeedTick((n) => n + 1)} />
        <Feed project={project} issue={issue} tick={feedTick} />
      </div>
      <aside className="detail-side" aria-label="Properties">
        <Properties project={project} issue={issue} save={save} disabled={!canWrite} />
        {canDelete && (
          <div style={{ marginTop: 16 }}>
            <button type="button" className="btn danger small" onClick={() => setConfirmDelete(true)}>
              <IconTrash size={14} /> Delete work item
            </button>
          </div>
        )}
      </aside>
      {confirmDelete && (
        <ConfirmDialog
          title="Delete work item?"
          message={<p>This permanently deletes <strong>{itemKey}</strong> with its comments, links and history. Sub-items are kept but lose their parent.</p>}
          confirmLabel="Delete"
          onClose={() => setConfirmDelete(false)}
          onConfirm={async () => {
            await issuesApi.remove(project.id, issue.id);
            emitIssue({ type: "deleted", id: issue.id });
            void refresh(project.id, "cycles");
            void refresh(project.id, "modules");
            onDeleted();
          }}
        />
      )}
    </div>
  );
}

function TitleEditor({ issue, disabled, onSave }: { issue: Issue; disabled: boolean; onSave: (name: string) => Promise<void> }) {
  const [value, setValue] = useState(issue.name);
  const [error, setError] = useState("");
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => setValue(issue.name), [issue.name]);
  useEffect(() => {
    const el = ref.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = `${el.scrollHeight}px`;
    }
  }, [value]);
  const commit = async () => {
    const v = value.trim();
    if (!v) {
      setValue(issue.name);
      setError("The title cannot be empty.");
      return;
    }
    if (v === issue.name) return;
    setError("");
    try {
      await onSave(v);
    } catch (e) {
      setError(errorText(e));
    }
  };
  return (
    <>
      <textarea
        ref={ref} className="title-input" rows={1} value={value} disabled={disabled} aria-label="Title"
        onChange={(e) => setValue(e.target.value.replace(/\n/g, ""))} onBlur={() => void commit()}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            (e.target as HTMLTextAreaElement).blur();
          }
        }}
      />
      {error && <p className="err" role="alert">{error}</p>}
    </>
  );
}
