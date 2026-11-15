// A button that opens a searchable option list (single or multi select),
// optionally with "Create <text>".

import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { IconCheck } from "./Icons";
import { useLayer } from "./layers";

export interface Option {
  value: string;
  label: string;
  icon?: ReactNode;
}

interface PickerProps {
  options: Option[];
  selected: string[];
  multi?: boolean;
  onChange: (values: string[]) => void;
  /** Contents of the trigger button. */
  children: ReactNode;
  label: string;
  buttonClass?: string;
  disabled?: boolean;
  onCreate?: (text: string) => Promise<void>;
  placeholder?: string;
}

export function Picker({ options, selected, multi, onChange, children, label, buttonClass = "prop-btn", disabled, onCreate, placeholder }: PickerProps) {
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={btn} type="button" className={buttonClass} aria-haspopup="listbox" aria-expanded={open}
        aria-label={label} disabled={disabled} onClick={() => setOpen((o) => !o)}
      >
        {children}
      </button>
      {open && (
        <PickerPopover
          anchor={btn.current} options={options} selected={selected} multi={multi} label={label} placeholder={placeholder}
          onCreate={onCreate}
          onClose={() => {
            setOpen(false);
            btn.current?.focus();
          }}
          onChange={(v) => {
            onChange(v);
            if (!multi) {
              setOpen(false);
              btn.current?.focus();
            }
          }}
        />
      )}
    </>
  );
}

interface PopoverProps {
  anchor: HTMLElement | null;
  options: Option[];
  selected: string[];
  multi?: boolean;
  label: string;
  placeholder?: string;
  onChange: (v: string[]) => void;
  onClose: () => void;
  onCreate?: (text: string) => Promise<void>;
}

function PickerPopover({ anchor, options, selected, multi, label, placeholder, onChange, onClose, onCreate }: PopoverProps) {
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const [pos, setPos] = useState<{ top: number; left: number }>({ top: -9999, left: -9999 });
  const [createErr, setCreateErr] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const listId = useId();
  useLayer(onClose);

  const needle = q.trim().toLowerCase();
  const shown = needle ? options.filter((o) => o.label.toLowerCase().includes(needle)) : options;
  const canCreate = !!onCreate && !!needle && !options.some((o) => o.label.toLowerCase() === needle);
  const total = shown.length + (canCreate ? 1 : 0);

  useLayoutEffect(() => {
    if (!anchor || !ref.current) return;
    const r = anchor.getBoundingClientRect();
    const h = ref.current.offsetHeight;
    const w = ref.current.offsetWidth;
    let top = r.bottom + 4;
    if (top + h > window.innerHeight - 8) top = Math.max(8, r.top - h - 4);
    const left = Math.max(8, Math.min(r.left, window.innerWidth - w - 8));
    setPos({ top, left });
  }, [anchor, total]);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node) && !anchor?.contains(e.target as Node)) onClose();
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [anchor, onClose]);

  useEffect(() => setActive(0), [needle]);

  const pick = (v: string) => {
    if (multi) onChange(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v]);
    else onChange(selected[0] === v ? [] : [v]);
  };
  const create = async () => {
    if (!onCreate) return;
    setCreateErr("");
    try {
      await onCreate(q.trim());
      setQ("");
    } catch (e) {
      setCreateErr(e instanceof Error ? e.message : String(e));
    }
  };
  const choose = (i: number) => {
    if (i < shown.length) pick(shown[i].value);
    else if (canCreate) void create();
  };

  return createPortal(
    <div ref={ref} className="popover" style={{ top: pos.top, left: pos.left }}>
      <input
        className="input" autoFocus placeholder={placeholder ?? "Filter…"} value={q} aria-label={label}
        role="combobox" aria-expanded="true" aria-controls={listId}
        aria-activedescendant={total ? `${listId}-${active}` : undefined}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((a) => Math.min(total - 1, a + 1));
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((a) => Math.max(0, a - 1));
          } else if (e.key === "Enter") {
            e.preventDefault();
            if (total) choose(active);
          } else if (e.key === "Tab") {
            onClose();
          }
        }}
      />
      <ul className="opt-list" role="listbox" id={listId} aria-multiselectable={multi || undefined}>
        {shown.map((o, i) => (
          <li key={o.value} role="none">
            <button
              type="button" role="option" id={`${listId}-${i}`} className="opt" tabIndex={-1}
              aria-selected={selected.includes(o.value)} data-active={i === active}
              onMouseEnter={() => setActive(i)} onClick={() => pick(o.value)}
            >
              {o.icon}
              <span>{o.label}</span>
              {selected.includes(o.value) && <span className="check"><IconCheck size={14} /></span>}
            </button>
          </li>
        ))}
        {canCreate && (
          <li role="none">
            <button
              type="button" role="option" id={`${listId}-${shown.length}`} className="opt" tabIndex={-1} aria-selected={false}
              data-active={active === shown.length} onMouseEnter={() => setActive(shown.length)} onClick={() => void create()}
            >
              Create “{q.trim()}”
            </button>
          </li>
        )}
        {!total && <li className="opt-empty">No matches</li>}
      </ul>
      {createErr && <p className="err" style={{ padding: "0 8px" }} role="alert">{createErr}</p>}
    </div>,
    document.body,
  );
}
