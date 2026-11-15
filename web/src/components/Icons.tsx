// Hand-drawn 16px stroke icons. Decorative: always aria-hidden; the button
// that holds an icon carries the aria-label.

import type { ReactNode } from "react";
import type { Priority, StateGroup } from "../api/types";

function Svg({ children, size = 16, className }: { children: ReactNode; size?: number; className?: string }) {
  return (
    <svg
      width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" className={className}
    >
      {children}
    </svg>
  );
}

type P = { size?: number; className?: string };

export const IconPlus = (p: P) => <Svg {...p}><path d="M8 3v10M3 8h10" /></Svg>;
export const IconX = (p: P) => <Svg {...p}><path d="M4 4l8 8M12 4l-8 8" /></Svg>;
export const IconChevron = (p: P) => <Svg {...p}><path d="M4 6l4 4 4-4" /></Svg>;
export const IconMenu = (p: P) => <Svg {...p}><path d="M2.5 4h11M2.5 8h11M2.5 12h11" /></Svg>;
export const IconList = (p: P) => <Svg {...p}><path d="M5.5 4h8M5.5 8h8M5.5 12h8" /><path d="M2.5 4h.01M2.5 8h.01M2.5 12h.01" strokeWidth="2" /></Svg>;
export const IconBoard = (p: P) => <Svg {...p}><rect x="2" y="2.5" width="3.5" height="11" rx="1" /><rect x="6.25" y="2.5" width="3.5" height="7" rx="1" /><rect x="10.5" y="2.5" width="3.5" height="9" rx="1" /></Svg>;
export const IconSearch = (p: P) => <Svg {...p}><circle cx="7" cy="7" r="4.5" /><path d="M10.5 10.5L14 14" /></Svg>;
export const IconCheck = (p: P) => <Svg {...p}><path d="M3 8.5l3 3 7-7" /></Svg>;
export const IconTrash = (p: P) => <Svg {...p}><path d="M2.5 4.5h11M6 4.5V3h4v1.5M4 4.5l.7 9h6.6l.7-9" /></Svg>;
export const IconEdit = (p: P) => <Svg {...p}><path d="M10.5 2.5l3 3-8 8h-3v-3z" /></Svg>;
export const IconLink = (p: P) => <Svg {...p}><path d="M6.5 9.5l3-3M7 4.5l1-1a2.8 2.8 0 014 4l-1 1M9 11.5l-1 1a2.8 2.8 0 01-4-4l1-1" /></Svg>;
export const IconExpand = (p: P) => <Svg {...p}><path d="M9.5 2.5h4v4M6.5 13.5h-4v-4M13.5 2.5L9 7M2.5 13.5L7 9" /></Svg>;
export const IconCalendar = (p: P) => <Svg {...p}><rect x="2.5" y="3.5" width="11" height="10" rx="1.5" /><path d="M2.5 6.5h11M5.5 2v3M10.5 2v3" /></Svg>;
export const IconCycle = (p: P) => <Svg {...p}><path d="M13 8a5 5 0 11-1.5-3.6" /><path d="M13 2.5v2.5h-2.5" /></Svg>;
export const IconModule = (p: P) => <Svg {...p}><rect x="2.5" y="2.5" width="4.5" height="4.5" rx="1" /><rect x="9" y="2.5" width="4.5" height="4.5" rx="1" /><rect x="2.5" y="9" width="4.5" height="4.5" rx="1" /><rect x="9" y="9" width="4.5" height="4.5" rx="1" /></Svg>;
export const IconItems = (p: P) => <Svg {...p}><rect x="2.5" y="2.5" width="11" height="11" rx="2" /><path d="M5.5 8l2 2 3-4" /></Svg>;
export const IconSettings = (p: P) => <Svg {...p}><circle cx="8" cy="8" r="2" /><path d="M8 1.8v1.7M8 12.5v1.7M1.8 8h1.7M12.5 8h1.7M3.6 3.6l1.2 1.2M11.2 11.2l1.2 1.2M3.6 12.4l1.2-1.2M11.2 4.8l1.2-1.2" /></Svg>;
export const IconLogout = (p: P) => <Svg {...p}><path d="M9.5 3H3.5v10h6M7 8h6.5M11 5.5L13.5 8 11 10.5" /></Svg>;
export const IconSub = (p: P) => <Svg {...p}><path d="M4 2.5v6a2 2 0 002 2h6M10 8.5l2 2-2 2" /></Svg>;
export const IconCopy = (p: P) => <Svg {...p}><rect x="5.5" y="5.5" width="8" height="8" rx="1.5" /><path d="M10.5 5.5v-2a1 1 0 00-1-1h-6a1 1 0 00-1 1v6a1 1 0 001 1h2" /></Svg>;
export const IconBack = (p: P) => <Svg {...p}><path d="M10 3L5 8l5 5" /></Svg>;

export function Logo({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" className="brand-mark">
      <rect x="2" y="2" width="20" height="20" rx="6" fill="currentColor" opacity="0.16" />
      <path d="M7 8h10M12 8v9" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" fill="none" />
      <circle cx="17" cy="16" r="2" fill="currentColor" />
    </svg>
  );
}

/** A filled-fraction circle that shows the state group. */
export function StateIcon({ group, color, size = 14 }: { group: StateGroup | undefined; color: string | undefined; size?: number }) {
  const c = color || "currentColor";
  const frac = { backlog: 0, unstarted: 0, started: 0.5, completed: 1, cancelled: 1 }[group ?? "backlog"];
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true" style={{ flex: "none" }}>
      <circle cx="8" cy="8" r="6.2" fill="none" stroke={c} strokeWidth="1.6" strokeDasharray={group === "backlog" ? "2.2 2" : undefined} />
      {group === "started" && <path d="M8 3.6A4.4 4.4 0 018 12.4z" fill={c} />}
      {group === "completed" && (
        <>
          <circle cx="8" cy="8" r="6.2" fill={c} />
          <path d="M5.2 8.2l1.9 1.9 3.7-3.8" stroke="var(--surface)" strokeWidth="1.6" fill="none" strokeLinecap="round" />
        </>
      )}
      {group === "cancelled" && frac === 1 && (
        <>
          <circle cx="8" cy="8" r="6.2" fill={c} />
          <path d="M5.8 5.8l4.4 4.4M10.2 5.8l-4.4 4.4" stroke="var(--surface)" strokeWidth="1.6" strokeLinecap="round" />
        </>
      )}
    </svg>
  );
}

export function PriorityIcon({ priority, size = 14 }: { priority: Priority; size?: number }) {
  if (priority === "urgent") {
    return (
      <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
        <rect x="1.5" y="1.5" width="13" height="13" rx="3" fill="currentColor" />
        <path d="M8 4.5v4.5M8 11.3v.2" stroke="var(--surface)" strokeWidth="1.8" strokeLinecap="round" />
      </svg>
    );
  }
  const bars = { high: 3, medium: 2, low: 1, none: 0 }[priority];
  if (bars === 0) {
    return (
      <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
        <path d="M3 8h2M7 8h2M11 8h2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    );
  }
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
      {[0, 1, 2].map((i) => (
        <rect key={i} x={2.5 + i * 4} y={10 - i * 3} width="2.6" height={4 + i * 3} rx="0.8" fill="currentColor" opacity={i < bars ? 1 : 0.25} />
      ))}
    </svg>
  );
}
