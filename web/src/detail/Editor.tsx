// Loads the TipTap editor on demand; read-only views never load it.

import { lazy, Suspense } from "react";
import type { RichEditorProps } from "./RichEditor";

const RichEditor = lazy(() => import("./RichEditor"));

/** Server-sanitized HTML (description_html / comment_html only). */
export function SafeHtml({ html, empty }: { html: string; empty?: string }) {
  if (!html || html.trim() === "<p></p>") return <p className="muted">{empty ?? ""}</p>;
  return <div className="prose" dangerouslySetInnerHTML={{ __html: html }} />;
}

export function Editor(props: RichEditorProps) {
  return (
    <Suspense fallback={<div className="editor"><div style={{ padding: "10px 12px" }}><SafeHtml html={props.initialHtml} /><span className="muted small">Loading editor…</span></div></div>}>
      <RichEditor {...props} />
    </Suspense>
  );
}
