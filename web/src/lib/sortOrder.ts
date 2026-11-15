// Board ordering. Cards in a column are sorted by sort_order ascending;
// a drop computes one new sort_order from the neighbours.

export const SORT_STEP = 10000;
export const SORT_DEFAULT = 65535;

/** sort_order for a card placed between prev and next (either may be missing). */
export function sortOrderBetween(prev: number | undefined, next: number | undefined): number {
  if (prev === undefined && next === undefined) return SORT_DEFAULT;
  if (prev === undefined) return (next as number) - SORT_STEP;
  if (next === undefined) return prev + SORT_STEP;
  if (prev === next) return prev; // degenerate; caller keeps order by tiebreak
  return (prev + next) / 2;
}

export interface Card {
  id: string;
  sort_order: number;
}

export function sortCards<T extends Card>(cards: T[]): T[] {
  return [...cards].sort((a, b) => a.sort_order - b.sort_order || a.id.localeCompare(b.id));
}

export interface DropResult {
  column: string;
  sortOrder: number;
}

/**
 * Where a dragged card lands. `columns` maps column id -> its cards in
 * display order (the dragged card may be in one of them). `over` is either a
 * column id or a card id; `after` says the pointer is in the lower half of
 * that card. Returns null when nothing would change.
 */
export function computeDrop(
  columns: Record<string, Card[]>,
  activeId: string,
  over: { column: string; cardId?: string; after?: boolean },
): DropResult | null {
  const target = columns[over.column];
  if (!target) return null;
  const from = Object.keys(columns).find((c) => columns[c].some((x) => x.id === activeId));
  const rest = target.filter((c) => c.id !== activeId);
  let index = rest.length;
  if (over.cardId && over.cardId !== activeId) {
    const i = rest.findIndex((c) => c.id === over.cardId);
    if (i >= 0) index = over.after ? i + 1 : i;
  } else if (over.cardId === activeId) {
    return null; // dropped on itself
  }
  if (from === over.column) {
    const oldIndex = target.findIndex((c) => c.id === activeId);
    if (oldIndex === index) return null;
  }
  const prev = rest[index - 1]?.sort_order;
  const next = rest[index]?.sort_order;
  return { column: over.column, sortOrder: sortOrderBetween(prev, next) };
}
