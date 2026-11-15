// Cmd/Ctrl+K search palette backed by the work item search endpoint.

import { useEffect, useId, useState } from "react";
import { useLocation } from "wouter";
import { errorText } from "../api/client";
import { issues as issuesApi } from "../api/endpoints";
import type { SearchHit } from "../api/types";
import { useDebounced } from "../components/bits";
import { Dialog } from "../components/Dialog";

export default function Palette({ onClose }: { onClose: () => void }) {
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [state, setState] = useState<"idle" | "loading" | "done" | "error">("idle");
  const [error, setError] = useState("");
  const [active, setActive] = useState(0);
  const [, navigate] = useLocation();
  const dq = useDebounced(q.trim(), 180);
  const listId = useId();

  useEffect(() => {
    if (!dq) {
      setHits([]);
      setState("idle");
      return;
    }
    const ctrl = new AbortController();
    setState("loading");
    issuesApi.search(dq, ctrl.signal).then(
      (r) => {
        setHits(r.issues);
        setActive(0);
        setState("done");
      },
      (e) => {
        if ((e as Error).name === "AbortError") return;
        setError(errorText(e));
        setState("error");
      },
    );
    return () => ctrl.abort();
  }, [dq]);

  const go = (h: SearchHit) => {
    onClose();
    navigate(`/p/${h.project__identifier}/items/${h.project__identifier}-${h.sequence_id}`, { state: { panel: true } });
  };

  return (
    <Dialog title="Search work items" onClose={onClose} className="palette" bare wide>
      <div className="dialog-body">
        <input
          className="input" data-autofocus placeholder="Search by title or key, like PROJ-12" value={q} aria-label="Search work items"
          role="combobox" aria-expanded={hits.length > 0} aria-controls={listId}
          aria-activedescendant={hits.length ? `${listId}-${active}` : undefined}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setActive((a) => Math.min(hits.length - 1, a + 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(0, a - 1));
            } else if (e.key === "Enter" && hits[active]) {
              e.preventDefault();
              go(hits[active]);
            }
          }}
        />
        <ul className="opt-list" role="listbox" id={listId} style={{ maxHeight: 360 }}>
          {hits.map((h, i) => (
            <li key={h.id} role="none">
              <button type="button" role="option" id={`${listId}-${i}`} aria-selected={i === active} data-active={i === active} className="opt" tabIndex={-1}
                onMouseEnter={() => setActive(i)} onClick={() => go(h)}>
                <span className="muted mono small" style={{ minWidth: 70 }}>{h.project__identifier}-{h.sequence_id}</span>
                <span>{h.name}</span>
              </button>
            </li>
          ))}
          {state === "idle" && <li className="opt-empty">Type to search across all projects. Press Esc to close.</li>}
          {state === "loading" && !hits.length && <li className="opt-empty">Searching…</li>}
          {state === "done" && !hits.length && <li className="opt-empty">No work items match “{dq}”.</li>}
          {state === "error" && <li className="err" style={{ padding: 8 }} role="alert">{error}</li>}
        </ul>
      </div>
    </Dialog>
  );
}
