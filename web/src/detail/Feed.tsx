// Comments and activity, merged oldest first.

import { useCallback, useEffect, useState } from "react";
import { useProjectList } from "../api/cache";
import { errorText } from "../api/client";
import { activities as activitiesApi, comments as commentsApi } from "../api/endpoints";
import type { Activity, Comment, Issue, Project, User } from "../api/types";
import { useIsAdmin, useSession } from "../app/session";
import { Avatar, ErrorBox, Loading, PRIORITY_LABEL, userName } from "../components/bits";
import { ConfirmDialog } from "../components/Dialog";
import { IconEdit, IconTrash } from "../components/Icons";
import { formatDateTime } from "../lib/dates";
import { Editor, SafeHtml } from "./Editor";

type Entry = { kind: "comment"; at: string; c: Comment } | { kind: "activity"; at: string; a: Activity };

const FIELD_LABEL: Record<string, string> = {
  name: "title", state: "state", priority: "priority", start_date: "start date", target_date: "target date", parent: "parent",
};

function value(field: string, v: string | null): string {
  if (!v) return "";
  if (field === "priority") return PRIORITY_LABEL[v as keyof typeof PRIORITY_LABEL] ?? v;
  return v;
}

/** One line of plain text for an activity row. */
export function describeActivity(a: Activity): string {
  const f = a.field ?? "";
  if (!f && a.verb === "created") return a.comment || "created the work item";
  if (f === "description") return "updated the description";
  const label = FIELD_LABEL[f];
  if (label) {
    const o = value(f, a.old_value);
    const n = value(f, a.new_value);
    if (o && n) return `changed the ${label} from “${o}” to “${n}”`;
    if (n) return `set the ${label} to “${n}”`;
    if (o) return `removed the ${label} “${o}”`;
  }
  return a.comment || `${a.verb} ${f.replace(/_/g, " ")}`.trim();
}

export function Feed({ project, issue, tick }: { project: Project; issue: Issue; tick: number }) {
  const [entries, setEntries] = useState<Entry[] | undefined>();
  const [error, setError] = useState("");
  const members = useProjectList(project.id, "members");
  const users = new Map<string, User>((members.data ?? []).map((u) => [u.id, u]));

  const load = useCallback(async () => {
    try {
      const [cs, as] = await Promise.all([commentsApi.list(project.id, issue.id), activitiesApi.list(project.id, issue.id)]);
      const list: Entry[] = [
        ...cs.map((c): Entry => ({ kind: "comment", at: c.created_at, c })),
        ...as.map((a): Entry => ({ kind: "activity", at: a.created_at, a })),
      ];
      list.sort((x, y) => (x.at < y.at ? -1 : x.at > y.at ? 1 : 0));
      setEntries(list);
      setError("");
    } catch (e) {
      setError(errorText(e));
    }
  }, [project.id, issue.id]);
  useEffect(() => {
    void load();
  }, [load, tick]);

  return (
    <section className="section" aria-labelledby={`feed-${issue.id}`}>
      <div className="section-head"><h2 id={`feed-${issue.id}`}>Activity</h2></div>
      {error && <ErrorBox error={error} onRetry={() => void load()} />}
      {!entries && !error && <Loading />}
      {entries && (
        <ul className="feed">
          {entries.map((e) =>
            e.kind === "activity" ? (
              <li key={e.a.id} className="feed-activity">
                <span><strong>{userName(users.get(e.a.actor ?? ""))}</strong> {describeActivity(e.a)}</span>
                <span className="spacer" />
                <time className="when" dateTime={e.a.created_at}>{formatDateTime(e.a.created_at)}</time>
              </li>
            ) : (
              <CommentItem key={e.c.id} project={project} issue={issue} c={e.c} author={users.get(e.c.actor ?? "")} onChanged={load} />
            ),
          )}
        </ul>
      )}
      <div style={{ marginTop: 12 }}>
        <Editor
          label="Add a comment" initialHtml="" submitLabel="Comment"
          onSubmit={async (html) => {
            await commentsApi.create(project.id, issue.id, html);
            await load();
          }}
        />
      </div>
    </section>
  );
}

function CommentItem({ project, issue, c, author, onChanged }: { project: Project; issue: Issue; c: Comment; author: User | undefined; onChanged: () => Promise<void> }) {
  const { user } = useSession();
  const isAdmin = useIsAdmin();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const mine = c.actor === user.id || isAdmin;
  return (
    <li className="feed-comment">
      <div className="feed-comment-head">
        <Avatar user={author} />
        <strong>{userName(author)}</strong>
        <time className="muted small" dateTime={c.created_at}>{formatDateTime(c.created_at)}</time>
        {c.edited_at && <span className="muted small">(edited)</span>}
        <span className="spacer" />
        {mine && !editing && (
          <>
            <button type="button" className="icon-btn" aria-label="Edit comment" onClick={() => setEditing(true)}><IconEdit size={14} /></button>
            <button type="button" className="icon-btn" aria-label="Delete comment" onClick={() => setDeleting(true)}><IconTrash size={14} /></button>
          </>
        )}
      </div>
      <div className="feed-comment-body">
        {editing ? (
          <Editor
            label="Edit comment" initialHtml={c.comment_html} submitLabel="Save" autoFocus onCancel={() => setEditing(false)}
            onSubmit={async (html) => {
              await commentsApi.update(project.id, issue.id, c.id, html);
              setEditing(false);
              await onChanged();
            }}
          />
        ) : (
          <SafeHtml html={c.comment_html} />
        )}
      </div>
      {deleting && (
        <ConfirmDialog
          title="Delete comment?" message="This comment will be removed for everyone." confirmLabel="Delete"
          onClose={() => setDeleting(false)}
          onConfirm={async () => {
            await commentsApi.remove(project.id, issue.id, c.id);
            await onChanged();
          }}
        />
      )}
    </li>
  );
}
