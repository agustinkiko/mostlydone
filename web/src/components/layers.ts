// A stack of open layers (popover, dialog, panel, mobile nav). Esc closes
// only the top one, so a popover inside a dialog inside a panel behaves.

import { useEffect, useRef } from "react";

const stack: { id: number; close: () => void }[] = [];
let nextId = 1;
let installed = false;

function install() {
  if (installed) return;
  installed = true;
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || !stack.length || e.defaultPrevented) return;
    e.preventDefault();
    stack[stack.length - 1].close();
  });
}

export function hasOpenLayer(): boolean {
  return stack.length > 0;
}

/** While `active`, Esc calls onClose (if this is the topmost layer). */
export function useLayer(onClose: () => void, active = true) {
  const ref = useRef(onClose);
  ref.current = onClose;
  useEffect(() => {
    if (!active) return;
    install();
    const id = nextId++;
    stack.push({ id, close: () => ref.current() });
    return () => {
      const i = stack.findIndex((l) => l.id === id);
      if (i >= 0) stack.splice(i, 1);
    };
  }, [active]);
}

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]';

/** Keeps Tab inside the element and restores focus to the opener on close. */
export function useFocusTrap(ref: React.RefObject<HTMLElement | null>, active = true) {
  useEffect(() => {
    if (!active) return;
    const el = ref.current;
    if (!el) return;
    const opener = document.activeElement as HTMLElement | null;
    const auto = el.querySelector<HTMLElement>("[data-autofocus]") ?? el.querySelector<HTMLElement>(FOCUSABLE);
    (auto ?? el).focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const items = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.offsetParent !== null || x === document.activeElement);
      if (!items.length) {
        e.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    el.addEventListener("keydown", onKey);
    return () => {
      el.removeEventListener("keydown", onKey);
      if (opener && document.contains(opener)) opener.focus();
    };
  }, [ref, active]);
}
