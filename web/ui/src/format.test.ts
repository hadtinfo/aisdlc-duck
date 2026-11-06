import { describe, expect, it } from "vitest";
import { ago, budgetLevel, labelText } from "./format";

const now = new Date("2026-11-03T12:00:00Z");
const before = (ms: number) => new Date(now.getTime() - ms).toISOString();

describe("ago", () => {
  it("30 giây là vừa xong", () => expect(ago(before(30_000), now)).toBe("vừa xong"));
  it("5 phút", () => expect(ago(before(5 * 60_000), now)).toBe("5 phút trước"));
  it("3 giờ", () => expect(ago(before(3 * 3600_000), now)).toBe("3 giờ trước"));
  it("2 ngày", () => expect(ago(before(2 * 86400_000), now)).toBe("2 ngày trước"));
});

describe("budgetLevel", () => {
  it("ok khi ≤1×", () => expect(budgetLevel(40, 40)).toBe("ok"));
  it("over khi ≤1,5×", () => expect(budgetLevel(60, 40)).toBe("over"));
  it("far khi >1,5×", () => expect(budgetLevel(61, 40)).toBe("far"));
  it("budget 0 là ok", () => expect(budgetLevel(10, 0)).toBe("ok"));
});

describe("labelText", () => {
  it("AWAITING_ACCEPT", () => expect(labelText("AWAITING_ACCEPT:S2")).toBe("Chờ accept S2"));
  it("các nhãn khác", () => {
    expect(labelText("AWAITING_APPROVAL:BRD")).toBe("Chờ duyệt BRD");
    expect(labelText("REJECTED:DESIGN")).toBe("Bị trả lại DESIGN");
    expect(labelText("STALE:CASES")).toBe("Đã đổi sau duyệt (STALE) CASES");
    expect(labelText("FINDINGS:BRD")).toBe("Có phát hiện BRD");
    expect(labelText("CODING:S2")).toBe("Đang code S2");
    expect(labelText("EVAL_FAIL")).toBe("Eval không đạt");
  });
});
