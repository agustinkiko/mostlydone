// The work item detail as a right-side panel (opened from a list) or as a
// full page (direct load of the URL).

import { useCallback, useRef } from "react";
import { Link, useLocation, useSearch } from "wouter";
import type { Project } from "../api/types";
import { TopBar } from "../app/Shell";
import { IconBack, IconExpand, IconX } from "../components/Icons";
import { useFocusTrap, useLayer } from "../components/layers";
import { ItemDetail } from "./ItemDetail";

export function DetailPanel({ project, itemKey, base }: { project: Project; itemKey: string; base: string }) {
  const [, navigate] = useLocation();
  const search = useSearch();
  const ref = useRef<HTMLDivElement>(null);
  const listHref = `${base}${search ? "?" + search : ""}`;
  const close = useCallback(() => {
    const st = window.history.state as { panel?: boolean } | null;
    if (st?.panel && window.history.length > 1) window.history.back();
    else navigate(listHref, { replace: true });
  }, [navigate, listHref]);
  useLayer(close);
  useFocusTrap(ref);
  return (
    <>
      <div className="panel-backdrop" onClick={close} />
      <div ref={ref} className="panel" role="dialog" aria-modal="true" aria-label={`Work item ${itemKey}`} tabIndex={-1}>
        <div className="panel-head">
          <span className="muted mono small">{itemKey}</span>
          <span className="spacer" />
          <Link href={`${base}/${itemKey}`} className="icon-btn" aria-label="Open as full page" title="Open as full page"
            onClick={(e) => { e.preventDefault(); navigate(`${base}/${itemKey}`, { replace: true, state: null }); }}>
            <IconExpand />
          </Link>
          <button type="button" className="icon-btn" aria-label="Close panel" onClick={close}><IconX /></button>
        </div>
        <div className="detail-scroll">
          <ItemDetail key={itemKey} project={project} itemKey={itemKey} onDeleted={() => navigate(listHref, { replace: true })} />
        </div>
      </div>
    </>
  );
}

export function DetailPage({ project, itemKey, backHref }: { project: Project; itemKey: string; backHref: string }) {
  const [, navigate] = useLocation();
  return (
    <>
      <TopBar title={<span className="row-gap"><Link href={backHref} className="icon-btn" aria-label="Back to work items"><IconBack /></Link>{itemKey}</span>} />
      <div className="page">
        <ItemDetail key={itemKey} project={project} itemKey={itemKey} onDeleted={() => navigate(backHref, { replace: true })} />
      </div>
    </>
  );
}
