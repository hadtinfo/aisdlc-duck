import { describe, expect, it } from "vitest";
import { actionsFor, approvePlan, canAct } from "./approve";
import type { GateData, ReviewCardData } from "./types";

const gate: GateData = {
  gate: "BRD", state: "AWAITING_APPROVAL", currentHash: "abcdef0123456789",
  submission: null, approval: null, lastFeedback: null,
};

const card: ReviewCardData = {
  gate: "BRD",
  flags: [
    { code: "ASSUMPTIONS_EMPTY", level: "INFO", message: "m", refs: [] },
    { code: "BRD_RULE_NO_ID", level: "WARN", message: "m", refs: [] },
    { code: "X_NA", level: "NA", message: "m", refs: [] },
    { code: "CASE_VALUE_NOT_IN_BRD", level: "HIGH", message: "m", refs: [] },
  ],
  assumptions: [{ text: "a0", source: "" }, { text: "a1", source: "" }, { text: "a2", source: "" }],
};

describe("approvePlan", () => {
  it("lấy cờ HIGH/WARN, nặng trước, bỏ INFO/NA; có tiêu đề", () => {
    const p = approvePlan(card, gate, []);
    expect(p.flags).toEqual([
      { code: "CASE_VALUE_NOT_IN_BRD", level: "HIGH", title: "Số liệu không có trong BRD" },
      { code: "BRD_RULE_NO_ID", level: "WARN", title: "Quy tắc thiếu ID" },
    ]);
  });
  it("hashShort 8 ký tự", () => expect(approvePlan(card, gate, []).hashShort).toBe("abcdef01"));
  it("giả định chưa xem = tổng trừ đã tick; seenTexts theo thứ tự", () => {
    const p = approvePlan(card, gate, [2, 0]);
    expect(p.unseenAssumptions).toBe(1);
    expect(p.seenTexts).toEqual(["a0", "a2"]);
  });
  it("chỉ số ngoài phạm vi hoặc trùng bị bỏ", () => {
    const p = approvePlan(card, gate, [1, 1, 7, -1]);
    expect(p.unseenAssumptions).toBe(2);
    expect(p.seenTexts).toEqual(["a1"]);
  });
  it("không có thẻ → không cờ, không giả định", () => {
    expect(approvePlan(null, gate, [0])).toEqual({ hashShort: "abcdef01", flags: [], unseenAssumptions: 0, seenTexts: [] });
  });
});

describe("canAct", () => {
  it("chủ tính năng", () => expect(canAct({ login: "dev", role: "dev" }, "dev")).toBe(true));
  it("lead", () => expect(canAct({ login: "lead", role: "lead" }, "dev")).toBe(true));
  it("người khác", () => expect(canAct({ login: "x", role: "dev" }, "dev")).toBe(false));
  it("không có chủ, không phải lead", () => expect(canAct({ login: "x", role: "dev" }, null)).toBe(false));
});

describe("actionsFor", () => {
  it("chờ duyệt: duyệt + trả lại", () =>
    expect(actionsFor("AWAITING_APPROVAL")).toEqual({ approve: true, reject: true, reopen: false }));
  it("có phát hiện: chỉ trả lại", () =>
    expect(actionsFor("SUBMITTED_WITH_FINDINGS")).toEqual({ approve: false, reject: true, reopen: false }));
  it("đã duyệt / STALE: mở lại", () => {
    expect(actionsFor("APPROVED")).toEqual({ approve: false, reject: false, reopen: true });
    expect(actionsFor("STALE")).toEqual({ approve: false, reject: false, reopen: true });
  });
  it("chưa nộp: không gì", () =>
    expect(actionsFor("NOT_SUBMITTED")).toEqual({ approve: false, reject: false, reopen: false }));
});
