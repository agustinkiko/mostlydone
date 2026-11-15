import { describe, expect, it } from "vitest";
import { activeFilterCount, emptyFilters, filtersToParams, filtersToQuery, parseFilters, toggleValue } from "./filters";
import { computeDrop, sortCards, sortOrderBetween, SORT_DEFAULT } from "./sortOrder";
import { cycleStatus, dayOf, formatDay, isOverdue, progress, todayStr } from "./dates";

describe("filters", () => {
  it("round-trips through the URL", () => {
    const f = { ...emptyFilters(), search: "login bug", priority: ["urgent", "high"], label: ["l1"], cycle: ["c1"] };
    const q = filtersToQuery(f);
    expect(q).toBe("search=login+bug&priority=urgent%2Chigh&label=l1&cycle=c1");
    expect(parseFilters("?" + q)).toEqual(f);
  });
  it("ignores junk and duplicates", () => {
    const f = parseFilters("priority=high,,high, low&foo=bar&search=%20x%20");
    expect(f.priority).toEqual(["high", "low"]);
    expect(f.search).toBe("x");
    expect(f.state_group).toEqual([]);
  });
  it("maps to REST params and applies fixed values", () => {
    const f = { ...emptyFilters(), assignee: ["u1", "u2"], cycle: ["other"] };
    expect(filtersToParams(f, { cycle: "c9" })).toEqual({ assignee: "u1,u2", cycle: "c9" });
    expect(filtersToParams(emptyFilters())).toEqual({});
  });
  it("counts and toggles", () => {
    expect(activeFilterCount({ ...emptyFilters(), search: "a", module: ["m"] })).toBe(2);
    expect(toggleValue(["a"], "b")).toEqual(["a", "b"]);
    expect(toggleValue(["a", "b"], "a")).toEqual(["b"]);
  });
});

describe("sort order", () => {
  it("computes between neighbours", () => {
    expect(sortOrderBetween(undefined, undefined)).toBe(SORT_DEFAULT);
    expect(sortOrderBetween(undefined, 100)).toBe(100 - 10000);
    expect(sortOrderBetween(100, undefined)).toBe(10100);
    expect(sortOrderBetween(100, 200)).toBe(150);
  });
  const cols = {
    a: [
      { id: "1", sort_order: 10 },
      { id: "2", sort_order: 20 },
      { id: "3", sort_order: 30 },
    ],
    b: [{ id: "4", sort_order: 5 }],
    c: [],
  };
  it("moves to an empty column", () => {
    expect(computeDrop(cols, "1", { column: "c" })).toEqual({ column: "c", sortOrder: SORT_DEFAULT });
  });
  it("moves to the end of a column", () => {
    expect(computeDrop(cols, "1", { column: "b" })).toEqual({ column: "b", sortOrder: 10005 });
  });
  it("moves before / after a card in another column", () => {
    expect(computeDrop(cols, "1", { column: "b", cardId: "4" })).toEqual({ column: "b", sortOrder: 5 - 10000 });
    expect(computeDrop(cols, "1", { column: "b", cardId: "4", after: true })).toEqual({ column: "b", sortOrder: 10005 });
  });
  it("reorders inside a column", () => {
    expect(computeDrop(cols, "1", { column: "a", cardId: "3", after: true })).toEqual({ column: "a", sortOrder: 10030 });
    expect(computeDrop(cols, "3", { column: "a", cardId: "1" })).toEqual({ column: "a", sortOrder: 10 - 10000 });
    expect(computeDrop(cols, "3", { column: "a", cardId: "2" })).toEqual({ column: "a", sortOrder: 15 });
  });
  it("returns null when nothing changes", () => {
    expect(computeDrop(cols, "2", { column: "a", cardId: "2" })).toBeNull();
    expect(computeDrop(cols, "2", { column: "a", cardId: "1", after: true })).toBeNull();
    expect(computeDrop(cols, "3", { column: "a" })).toBeNull();
    expect(computeDrop(cols, "1", { column: "zzz" })).toBeNull();
  });
  it("sorts cards stably", () => {
    expect(sortCards([{ id: "b", sort_order: 1 }, { id: "a", sort_order: 1 }, { id: "c", sort_order: 0 }]).map((c) => c.id)).toEqual(["c", "a", "b"]);
  });
});

describe("dates", () => {
  const today = "2026-11-14";
  it("reads day parts", () => {
    expect(dayOf("2026-11-01T00:00:00.000000Z")).toBe("2026-11-01");
    expect(dayOf(null)).toBeNull();
    expect(dayOf("garbage")).toBeNull();
  });
  it("formats", () => {
    expect(formatDay("2026-11-04", today)).toBe("Nov 4");
    expect(formatDay("2025-01-30", today)).toBe("Jan 30, 2025");
    expect(todayStr(new Date(2026, 0, 5))).toBe("2026-01-05");
  });
  it("detects overdue", () => {
    expect(isOverdue("2026-11-13", "started", today)).toBe(true);
    expect(isOverdue("2026-11-14", "started", today)).toBe(false);
    expect(isOverdue("2026-11-01", "completed", today)).toBe(false);
    expect(isOverdue(null, "started", today)).toBe(false);
  });
  it("derives cycle status", () => {
    expect(cycleStatus(null, null, today)).toBe("draft");
    expect(cycleStatus("2026-11-20T00:00:00.000000Z", "2026-11-30T23:59:59.000000Z", today)).toBe("upcoming");
    expect(cycleStatus("2026-11-01T00:00:00.000000Z", "2026-11-14T23:59:59.000000Z", today)).toBe("current");
    expect(cycleStatus("2026-10-01T00:00:00.000000Z", "2026-10-31T23:59:59.000000Z", today)).toBe("completed");
  });
  it("computes progress", () => {
    expect(progress({ total_issues: 0, completed_issues: 0 })).toBe(0);
    expect(progress({ total_issues: 3, completed_issues: 1 })).toBe(33);
  });
});
