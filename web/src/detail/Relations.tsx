// Sub-items and links of a work item.

import { useEffect, useId, useState, type FormEvent } from "react";
import { useLocation } from "wouter";
import { errorText } from "../api/client";
import { issues as issuesApi, links as linksApi } from "../api/endpoints";
import type { Issue, IssueLink, Project } from "../api/types";
import { issueKey, useCanWrite } from "../app/session";
import { ErrorBox, Loading } from "../components/bits";
import { IconLink, IconPlus, IconTrash, StateIcon } from "../components/Icons";
import { applyIssueEvent, onIssue } from "../lib/issueEvents";
import { sortCards } from "../lib/sortOrder";
import { ItemMeta, onItemClick, QuickAdd, useLookups } from "../pages/itemBits";

export function SubItems({ project, issue }: { project: Project; issue: Issue }) {
  const [items, setItems] = useState<Issue[] | undefined>();
  const [error, setError] = useState("");
  const [location, navigate] = useLocation();
  const lk = useLookups(project.id);
  const canWrite = useCanWrite();

  useEffect(() => {
    let live = true;
    issuesApi.list(project.id, { parent: issue.id }, 500).then(
      (r) => live && setItems(r.items),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [project.id, issue.id]);

  useEffect(
    () =>
      onIssue((e) =>
        setItems((list) => {
          if (!list) return list;
          if (e.type === "saved" && e.issue.parent !== issue.id) return list.filter((i) => i.id !== e.issue.id);
          return applyIssueEvent(list, e, (i) => i.parent === issue.id);
        }),
      ),
    [issue.id],
  );

  const hrefFor = (key: string) => location.replace(/[^/]+$/, key);

  return (
    <section className="section" aria-labelledby={`sub-${issue.id}`}>
      <div className="section-head">
        <h2 id={`sub-${issue.id}`}>Sub-items</h2>
        {items && <span className="muted small">{items.length}</span>}
      </div>
      {error && <ErrorBox error={error} />}
      {!items && !error && <Loading />}
      {items && items.length === 0 && <p className="muted small">No sub-items.</p>}
      {items &&
        sortCards(items).map((i) => {
          const key = issueKey(project, i.sequence_id);
          const st = lk.stateById.get(i.state);
          return (
            <a key={i.id} href={hrefFor(key)} className="sub-row"
              onClick={(e) => onItemClick(e, () => navigate(hrefFor(key), { state: window.history.state }))}>
              <StateIcon group={st?.group} color={st?.color} />
              <span className="item-key">{key}</span>
              <span className="item-name">{i.name}</span>
              <span className="item-meta"><ItemMeta issue={i} lk={lk} /></span>
            </a>
          );
        })}
      {canWrite && <QuickAdd project={project} defaults={{ parent: issue.id }} className="sub-row" />}
    </section>
  );
}

export function Links({ project, issue, onChange }: { project: Project; issue: Issue; onChange: () => void }) {
  const [list, setList] = useState<IssueLink[] | undefined>();
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [formErr, setFormErr] = useState("");
  const [busy, setBusy] = useState(false);
  const canWrite = useCanWrite();
  const ids = { url: useId(), title: useId() };

  useEffect(() => {
    let live = true;
    linksApi.list(project.id, issue.id).then(
      (l) => live && setList(l),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [project.id, issue.id]);

  const add = async (e: FormEvent) => {
    e.preventDefault();
    let u = url.trim();
    if (!u) return;
    if (!/^https?:\/\//i.test(u)) u = `https://${u}`;
    setBusy(true);
    setFormErr("");
    try {
      const l = await linksApi.create(project.id, issue.id, u, title.trim());
      setList((cur) => [l, ...(cur ?? [])]);
      setUrl("");
      setTitle("");
      setAdding(false);
      onChange();
    } catch (x) {
      setFormErr(errorText(x));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (l: IssueLink) => {
    setError("");
    try {
      await linksApi.remove(project.id, issue.id, l.id);
      setList((cur) => cur?.filter((x) => x.id !== l.id));
      onChange();
    } catch (x) {
      setError(errorText(x));
    }
  };

  return (
    <section className="section" aria-labelledby={`links-${issue.id}`}>
      <div className="section-head">
        <h2 id={`links-${issue.id}`}>Links</h2>
        <span className="spacer" />
        {canWrite && !adding && (
          <button type="button" className="btn small ghost" onClick={() => setAdding(true)}><IconPlus size={14} /> Add link</button>
        )}
      </div>
      {error && <ErrorBox error={error} />}
      {!list && !error && <Loading />}
      {list && list.length === 0 && !adding && <p className="muted small">No links.</p>}
      {list?.map((l) => (
        <div key={l.id} className="sub-row">
          <IconLink size={14} />
          <a href={l.url} target="_blank" rel="noopener noreferrer nofollow" className="item-name" title={l.url}>{l.title || l.url}</a>
          {canWrite && (
            <button type="button" className="icon-btn" aria-label={`Remove link ${l.title || l.url}`} onClick={() => void remove(l)}>
              <IconTrash size={14} />
            </button>
          )}
        </div>
      ))}
      {adding && (
        <form onSubmit={add} className="list-card" style={{ padding: 10, marginTop: 6 }}>
          <div className="field">
            <label htmlFor={ids.url}>URL</label>
            <input id={ids.url} className="input" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" autoFocus />
          </div>
          <div className="field">
            <label htmlFor={ids.title}>Title (optional)</label>
            <input id={ids.title} className="input" value={title} onChange={(e) => setTitle(e.target.value)} />
          </div>
          {formErr && <p className="err" role="alert">{formErr}</p>}
          <div className="row-gap">
            <button type="submit" className="btn primary small" disabled={busy || !url.trim()}>{busy ? "Adding…" : "Add link"}</button>
            <button type="button" className="btn small" onClick={() => { setAdding(false); setFormErr(""); }}>Cancel</button>
          </div>
        </form>
      )}
    </section>
  );
}
