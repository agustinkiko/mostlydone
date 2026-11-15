import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, buildUrl, cursorPage, fetchAllPages, parseError, request, setUnauthorizedHandler } from "./client";
import type { Page } from "./types";

describe("parseError", () => {
  it("uses error, detail and message keys", () => {
    expect(parseError(400, { error: "Default state cannot be deleted" }).message).toBe("Default state cannot be deleted");
    expect(parseError(403, { detail: "You do not have permission to perform this action." }).message).toMatch(/permission/);
    expect(parseError(400, { code: "CYCLE_COMPLETED", message: "The Cycle has already been completed" }).message).toMatch(/completed/);
  });
  it("collects field errors in both list and string form", () => {
    const e = parseError(400, { name: ["This field is required."], start_date: ["Bad date."] });
    expect(e.fields).toEqual({ name: "This field is required.", start_date: "Bad date." });
    expect(e.message).toBe("Name: This field is required. Start date: Bad date.");
    const c = parseError(409, { identifier: "The project identifier is already taken" });
    expect(c.fields.identifier).toBe("The project identifier is already taken");
  });
  it("hides non_field_errors label and ignores id/code", () => {
    expect(parseError(400, { non_field_errors: ["Start date cannot exceed target date"] }).message).toBe(
      "Start date cannot exceed target date",
    );
    expect(parseError(409, { error: "dup", id: "x" }).message).toBe("dup");
  });
  it("falls back by status", () => {
    expect(parseError(500, null).message).toMatch(/server/);
    expect(parseError(404, []).message).toBe("Not found.");
    expect(parseError(502, "Bad Gateway").message).toBe("Bad Gateway");
  });
});

describe("buildUrl and cursors", () => {
  it("drops empty params", () => {
    expect(buildUrl("/x/", { a: "1", b: "", c: undefined, d: 0 })).toBe("/x/?a=1&d=0");
    expect(buildUrl("/x/")).toBe("/x/");
  });
  it("reads cursor page", () => {
    expect(cursorPage("100:2:0")).toBe(2);
    expect(cursorPage("100:-1:1")).toBe(-1);
    expect(cursorPage("bad")).toBe(-1);
  });
});

function page<T>(results: T[], pageIdx: number, total: number, perPage: number): Page<T> {
  return {
    total_count: total, total_results: total, total_pages: Math.ceil(total / perPage), count: results.length,
    next_cursor: `${perPage}:${pageIdx + 1}:0`, prev_cursor: `${perPage}:${pageIdx - 1}:1`,
    next_page_results: (pageIdx + 1) * perPage < total, prev_page_results: pageIdx > 0, results,
  };
}

describe("fetchAllPages", () => {
  const data = Array.from({ length: 7 }, (_, i) => i);
  const fetchPage = (perPage: number) => async (cursor: string) => {
    const idx = cursor ? cursorPage(cursor) : 0;
    return page(data.slice(idx * perPage, idx * perPage + perPage), idx, data.length, perPage);
  };
  it("follows next_cursor to the end", async () => {
    const cursors: string[] = [];
    const f = fetchPage(3);
    const r = await fetchAllPages((c) => (cursors.push(c), f(c)));
    expect(r.items).toEqual(data);
    expect(r.truncated).toBe(false);
    expect(cursors).toEqual(["", "3:1:0", "3:2:0"]);
  });
  it("stops at maxItems", async () => {
    const r = await fetchAllPages(fetchPage(3), 4);
    expect(r.items.length).toBe(6);
    expect(r.truncated).toBe(true);
    expect(r.total).toBe(7);
  });
});

describe("request", () => {
  afterEach(() => vi.unstubAllGlobals());
  it("sends the CSRF header and JSON body, maps errors, calls the 401 handler", async () => {
    const calls: { url: string; init: RequestInit }[] = [];
    vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      if (url === "/ok") return new Response(JSON.stringify({ a: 1 }), { status: 200 });
      if (url === "/gone") return new Response(null, { status: 204 });
      return new Response(JSON.stringify({ detail: "Session expired or invalid." }), { status: 401 });
    });
    let unauth = 0;
    setUnauthorizedHandler(() => unauth++);
    expect(await request("/ok", { method: "POST", body: { x: 1 } })).toEqual({ a: 1 });
    const h = calls[0].init.headers as Record<string, string>;
    expect(h["X-Requested-With"]).toBe("mostlydone");
    expect(h["Content-Type"]).toBe("application/json");
    expect(calls[0].init.body).toBe('{"x":1}');
    expect(await request("/gone", { method: "DELETE" })).toBeUndefined();
    await expect(request("/no")).rejects.toBeInstanceOf(ApiError);
    expect(unauth).toBe(1);
    await expect(request("/no", { allow401: true })).rejects.toThrow("Session expired or invalid.");
    expect(unauth).toBe(1);
  });
  it("reports network failures in plain words", async () => {
    vi.stubGlobal("fetch", async () => {
      throw new TypeError("Failed to fetch");
    });
    await expect(request("/x")).rejects.toThrow(/Could not reach the server/);
  });
});
