// TipTap editor (lazy chunk). Produces only tags the server's allow-list
// keeps: p, h1-h3, strong, em, s, u, code, pre, blockquote, ul/ol/li, a, br, hr.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { errorText } from "../api/client";
import { IconLink } from "../components/Icons";

export interface RichEditorProps {
  initialHtml: string;
  /** Description mode: saves on blur / Save and on unmount. */
  onSave?: (html: string) => Promise<void>;
  /** Comment mode: submit button, clears after success. */
  onSubmit?: (html: string) => Promise<void>;
  submitLabel?: string;
  onCancel?: () => void;
  autoFocus?: boolean;
  label: string;
}

const EMPTY = new Set(["", "<p></p>"]);

function normalize(html: string): string {
  return EMPTY.has(html.trim()) ? "" : html;
}

export default function RichEditor({ initialHtml, onSave, onSubmit, submitLabel = "Comment", onCancel, autoFocus, label }: RichEditorProps) {
  const [status, setStatus] = useState<"clean" | "dirty" | "saving" | "saved" | "error">("clean");
  const [error, setError] = useState("");
  const saved = useRef(normalize(initialHtml));
  const saveRef = useRef(onSave);
  saveRef.current = onSave;

  const editor = useEditor({
    extensions: [
      StarterKit.configure({
        heading: { levels: [1, 2, 3] },
        link: { openOnClick: false, autolink: true, protocols: ["http", "https", "mailto"], HTMLAttributes: { rel: "noopener noreferrer nofollow", target: "_blank" } },
      }),
    ],
    content: initialHtml || "",
    autofocus: autoFocus ? "end" : false,
    shouldRerenderOnTransaction: true,
    editorProps: { attributes: { "aria-label": label, role: "textbox", "aria-multiline": "true" } },
    onUpdate: ({ editor: ed }) => {
      if (onSave) setStatus(normalize(ed.getHTML()) === saved.current ? "clean" : "dirty");
    },
    onBlur: ({ editor: ed, event }) => {
      const to = (event as FocusEvent).relatedTarget as Node | null;
      const root = ed.view.dom.closest(".editor");
      if (to && root?.contains(to)) return; // moving to the toolbar
      if (onSave) void save(ed);
    },
  });

  async function save(ed: Editor) {
    const html = normalize(ed.getHTML());
    if (html === saved.current || !saveRef.current) return;
    setStatus("saving");
    setError("");
    try {
      await saveRef.current(html || "<p></p>");
      saved.current = html;
      setStatus(normalize(ed.getHTML()) === html ? "saved" : "dirty");
    } catch (e) {
      setStatus("error");
      setError(errorText(e));
    }
  }

  // Save pending description edits when the panel closes.
  useEffect(() => {
    return () => {
      if (!editor || editor.isDestroyed || !saveRef.current) return;
      const html = normalize(editor.getHTML());
      if (html !== saved.current) void saveRef.current(html || "<p></p>").catch(() => {});
    };
  }, [editor]);

  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!editor || !onSubmit || editor.isEmpty) return;
    setBusy(true);
    setError("");
    try {
      await onSubmit(editor.getHTML());
      if (!editor.isDestroyed) editor.commands.clearContent(true);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  if (!editor) return null;
  return (
    <div className={`editor${onSubmit ? " compact" : ""}`}
      onKeyDown={(e) => {
        if (onSubmit && e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          void submit();
        }
        if (onCancel && e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <Toolbar editor={editor} />
      <EditorContent editor={editor} className="prose" />
      <div className="editor-foot">
        {onSave && (
          <>
            <span className={status === "error" ? "err" : "muted small"} role="status" aria-live="polite">
              {status === "saving" ? "Saving…" : status === "saved" ? "Saved" : status === "dirty" ? "Unsaved changes" : status === "error" ? `Not saved: ${error}` : ""}
            </span>
            <span className="spacer" />
            <button type="button" className="btn small" disabled={status === "clean" || status === "saved" || status === "saving"} onClick={() => void save(editor)}>Save</button>
          </>
        )}
        {onSubmit && (
          <>
            {error ? <span className="err" role="alert">{error}</span> : <span className="muted small">Ctrl/⌘ Enter to send</span>}
            <span className="spacer" />
            {onCancel && <button type="button" className="btn small" onClick={onCancel}>Cancel</button>}
            <button type="button" className="btn primary small" disabled={busy || editor.isEmpty} onClick={() => void submit()}>
              {busy ? "Sending…" : submitLabel}
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function Toolbar({ editor }: { editor: Editor }) {
  const b = (label: string, text: ReactNode, active: boolean, run: () => void) => (
    <button type="button" className="icon-btn" aria-label={label} title={label} aria-pressed={active} onMouseDown={(e) => e.preventDefault()} onClick={run}>
      {text}
    </button>
  );
  const chain = () => editor.chain().focus();
  const setLink = () => {
    const prev = (editor.getAttributes("link").href as string | undefined) ?? "";
    const url = window.prompt("Link address (leave empty to remove)", prev);
    if (url === null) return;
    if (!url.trim()) chain().extendMarkRange("link").unsetLink().run();
    else if (/^(https?:\/\/|mailto:)/i.test(url.trim())) chain().extendMarkRange("link").setLink({ href: url.trim() }).run();
    else chain().extendMarkRange("link").setLink({ href: `https://${url.trim()}` }).run();
  };
  return (
    <div className="editor-toolbar" role="toolbar" aria-label="Formatting">
      {b("Bold", "B", editor.isActive("bold"), () => chain().toggleBold().run())}
      {b("Italic", "I", editor.isActive("italic"), () => chain().toggleItalic().run())}
      {b("Strikethrough", "S", editor.isActive("strike"), () => chain().toggleStrike().run())}
      {b("Inline code", "</>", editor.isActive("code"), () => chain().toggleCode().run())}
      {b("Heading", "H", editor.isActive("heading", { level: 2 }), () => chain().toggleHeading({ level: 2 }).run())}
      {b("Bullet list", "•", editor.isActive("bulletList"), () => chain().toggleBulletList().run())}
      {b("Numbered list", "1.", editor.isActive("orderedList"), () => chain().toggleOrderedList().run())}
      {b("Quote", "“", editor.isActive("blockquote"), () => chain().toggleBlockquote().run())}
      {b("Code block", "{}", editor.isActive("codeBlock"), () => chain().toggleCodeBlock().run())}
      {b("Link", <IconLink size={14} />, editor.isActive("link"), setLink)}
    </div>
  );
}
