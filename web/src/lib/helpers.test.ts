import { describe, expect, it } from "vitest";
import { textToHtml } from "../app/NewItemDialog";
import { suggestIdentifier } from "../app/Sidebar";
import { describeActivity } from "../detail/Feed";
import { applyIssueEvent } from "./issueEvents";
import { generalError, initials } from "../components/bits";
import { parseError } from "../api/client";
import type { Activity, Issue } from "../api/types";

const act = (p: Partial<Activity>): Activity => ({
  id: "a", verb: "updated", field: null, old_value: null, new_value: null, comment: "", actor: null, created_at: "", ...p,
});

describe("helpers", () => {
  it("escapes plain text into paragraphs", () => {
    expect(textToHtml("")).toBe("<p></p>");
    expect(textToHtml("a <b>\nline2\n\npara & 2")).toBe("<p>a &lt;b&gt;<br>line2</p><p>para &amp; 2</p>");
  });
  it("suggests identifiers", () => {
    expect(suggestIdentifier("Web site redesign")).toBe("WSR");
    expect(suggestIdentifier("backend")).toBe("BACKE");
    expect(suggestIdentifier("  ")).toBe("");
  });
  it("describes activity rows", () => {
    expect(describeActivity(act({ verb: "created", comment: "created the work item" }))).toBe("created the work item");
    expect(describeActivity(act({ field: "state", old_value: "Todo", new_value: "Done" }))).toBe("changed the state from “Todo” to “Done”");
    expect(describeActivity(act({ field: "priority", old_value: "none", new_value: "high" }))).toBe("changed the priority from “No priority” to “High”");
    expect(describeActivity(act({ field: "target_date", new_value: "2026-11-20" }))).toBe("set the target date to “2026-11-20”");
    expect(describeActivity(act({ field: "description", old_value: "<p>x</p>", new_value: "<p>y</p>" }))).toBe("updated the description");
    expect(describeActivity(act({ field: "labels", comment: "added labels bug" }))).toBe("added labels bug");
  });
  it("applies issue events", () => {
    const i = (id: string, name = id) => ({ id, name }) as Issue;
    const list = [i("1"), i("2")];
    expect(applyIssueEvent(list, { type: "deleted", id: "1" }, () => true).map((x) => x.id)).toEqual(["2"]);
    expect(applyIssueEvent(list, { type: "saved", issue: i("2", "new") }, () => false)[1].name).toBe("new");
    expect(applyIssueEvent(list, { type: "saved", issue: i("3") }, () => false)).toHaveLength(2);
    expect(applyIssueEvent(list, { type: "saved", issue: i("3") }, () => true)).toHaveLength(3);
  });
  it("splits field and general errors", () => {
    const e = parseError(400, { name: ["required"], non_field_errors: ["Start date cannot exceed target date"] });
    expect(generalError(e, ["name"])).toBe("Start date cannot exceed target date");
    expect(generalError(parseError(400, { name: ["required"] }), ["name"])).toBe("");
    expect(generalError(parseError(400, { error: "Default state cannot be deleted" }), ["name"])).toBe("Default state cannot be deleted");
  });
  it("makes initials", () => {
    expect(initials({ display_name: "Ada Lovelace" } as never)).toBe("AL");
    expect(initials({ display_name: "", first_name: "", last_name: "", email: "bob@x.io" } as never)).toBe("BO");
  });
});
