import { useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { errorText } from "../api/client";
import { IconX } from "./Icons";
import { useFocusTrap, useLayer } from "./layers";

interface DialogProps {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
  className?: string;
  /** Hide the visible header (the title stays as the accessible name). */
  bare?: boolean;
}

export function Dialog({ title, onClose, children, footer, wide, className, bare }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  const id = useId();
  useLayer(onClose);
  useFocusTrap(ref);
  return createPortal(
    <div className="dialog-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={ref} role="dialog" aria-modal="true" aria-labelledby={id} tabIndex={-1}
        className={`dialog${wide ? " wide" : ""}${className ? " " + className : ""}`}
      >
        {bare ? (
          <h2 id={id} className="sr-only">{title}</h2>
        ) : (
          <div className="dialog-head">
            <h2 id={id}>{title}</h2>
            <span className="spacer" />
            <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}><IconX /></button>
          </div>
        )}
        {children}
        {footer && <div className="dialog-foot">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

interface ConfirmProps {
  title: string;
  message: ReactNode;
  confirmLabel: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
  /** When set, the user must type this text to enable the button. */
  typeToConfirm?: string;
}

/** A destructive-action confirm step. Shows the server's refusal inline. */
export function ConfirmDialog({ title, message, confirmLabel, onConfirm, onClose, typeToConfirm }: ConfirmProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const ready = !typeToConfirm || typed === typeToConfirm;
  const run = async () => {
    setBusy(true);
    setError("");
    try {
      await onConfirm();
      onClose();
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="button" className="btn danger solid" disabled={!ready || busy} onClick={run}>
            {busy ? "Working…" : confirmLabel}
          </button>
        </>
      }
    >
      <div className="dialog-body">
        <div>{message}</div>
        {typeToConfirm && (
          <div className="field" style={{ marginTop: 12 }}>
            <label htmlFor={inputId}>Type <strong>{typeToConfirm}</strong> to confirm</label>
            <input id={inputId} className="input" value={typed} onChange={(e) => setTyped(e.target.value)} data-autofocus autoComplete="off" />
          </div>
        )}
        {error && <p className="err" role="alert">{error}</p>}
      </div>
    </Dialog>
  );
}
