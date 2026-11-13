import { describe, expect, it } from "vitest";
import { acceptStatusShown, actionLabel, actionRetryable, eventReloadsControl, feedbackLabel, INSTRUCTION_MAX, latestAction, outOfScopeFiles, validScopePath } from "./control";
import type { ControlAction, FeatureEvent, StepData } from "./types";

const act = (o: Partial<ControlAction>): ControlAction => ({
  id: 1, kind: "ACCEPT_STEP", step: "S1", path: null, by: "a", at: "2026-01-01T00:00:00Z",
  expiresAt: "2026-01-08T00:00:00Z", status: "PENDING", resolvedAt: null, reason: null, ...o,
});
const ev = (o: Partial<FeatureEvent>): FeatureEvent => ({
  type: "SCOPE_DENIED", at: "2026-01-01T00:00:00Z", step: "S1", path: "x", outOfScope: false, sender: null, detail: null, ...o,
});
const step = (oos?: string[]): StepData => ({
  id: "S1", title: "t", scope: [], budget: { files: 1, lines: 1 }, caseIds: [], status: "AWAITING_DEV",
  record: oos ? { check: { outOfScope: oos } } : null, diff: null,
});

describe("actionLabel", () => {
  it("các nhánh", () => {
    expect(actionLabel("ACCEPT_STEP", "PENDING")).toBe("Đã accept, chờ áp dụng");
    expect(actionLabel("ALLOW_SCOPE", "PENDING")).toBe("Đã cho phép, chờ áp dụng");
    expect(actionLabel("ACCEPT_STEP", "APPLIED")).toBe("Đã áp dụng");
    expect(actionLabel("ALLOW_SCOPE", "STALE", "đổi cây")).toBe("Hết hiệu lực: đổi cây — bấm lại");
    expect(actionLabel("ALLOW_SCOPE", "STALE")).toBe("Hết hiệu lực — bấm lại");
    expect(actionLabel("ACCEPT_STEP", "EXPIRED")).toBe("Hết hạn (7 ngày) — bấm lại");
    expect(actionLabel("ACCEPT_STEP", "CANCELLED")).toBe("Đã huỷ — bấm lại");
  });
});
describe("outOfScopeFiles", () => {
  it("gộp check và SCOPE_DENIED cùng bước, bỏ trùng, giữ thứ tự", () => {
    const events = [ev({ path: "c" }), ev({ path: "a" }), ev({ path: "z", step: "S2" }), ev({ type: "FILE_EDIT", path: "q" })];
    expect(outOfScopeFiles(step(["a", "b"]), events)).toEqual(["a", "b", "c"]);
    expect(outOfScopeFiles(step(), [])).toEqual([]);
  });
});
describe("latestAction", () => {
  it("chọn id lớn nhất đúng (kind, step, path)", () => {
    const l = [act({ id: 1 }), act({ id: 5 }), act({ id: 9, step: "S2" }), act({ id: 7, kind: "ALLOW_SCOPE", path: "p" }), act({ id: 8, kind: "ALLOW_SCOPE", path: "p" })];
    expect(latestAction(l, "ACCEPT_STEP", "S1")?.id).toBe(5);
    expect(latestAction(l, "ALLOW_SCOPE", "S1", "p")?.id).toBe(8);
    expect(latestAction(l, "ALLOW_SCOPE", "S1", "other")).toBeNull();
  });
});
describe("feedbackLabel", () => {
  it("ba giá trị", () => {
    expect(feedbackLabel("RELATED_CHANGED")).toBe("có thay đổi liên quan");
    expect(feedbackLabel("NO_CHANGE_SEEN")).toBe("chưa thấy thay đổi");
    expect(feedbackLabel("UNMATCHED")).toBe("không rõ mục nào");
    expect(INSTRUCTION_MAX).toBe(2000);
  });
});
describe("eventReloadsControl", () => {
  it("mọi event trừ FILE_EDIT", () => {
    expect(eventReloadsControl("ACTION_RESULT")).toBe(true);
    expect(eventReloadsControl("SCOPE_DENIED")).toBe(true);
    expect(eventReloadsControl("INSTRUCTION_DELIVERED")).toBe(true);
    expect(eventReloadsControl("FILE_EDIT")).toBe(false);
  });
});
describe("acceptStatusShown", () => {
  it("bước chờ dev: mọi trạng thái; bước đã accept: chỉ khi lệnh từ web đã áp dụng", () => {
    expect(acceptStatusShown("AWAITING_DEV", act({ status: "STALE" }))).toBe(true);
    expect(acceptStatusShown("ACCEPTED", act({ status: "APPLIED" }))).toBe(true);
    expect(acceptStatusShown("ACCEPTED", act({ status: "STALE" }))).toBe(false);
    expect(acceptStatusShown("IN_PROGRESS", act({ status: "APPLIED" }))).toBe(false);
    expect(acceptStatusShown("AWAITING_DEV", null)).toBe(false);
  });
});

describe("validScopePath (cùng luật PendingActions.validScopePath)", () => {
  it("nhận đường dẫn file cụ thể", () => {
    for (const p of ["src/a/B.java", "pom.xml", "a/.aisdlc/x", "src/a b.txt", "a".repeat(500)]) expect(validScopePath(p), p).toBe(true);
  });
  it("từ chối glob, ký tự đặc biệt, dạng chưa chuẩn hóa", () => {
    for (const p of ["", " ", "../x", "/x", "a\\b", ".aisdlc/x", ".aisdlc", "a".repeat(501), "a/../b", "**", ".aisdl?/**",
      "{.aisdlc,src}/**", "./.aisdlc/x", "C:/x", "a//b", "a/./b", "a/", "app/[id]/page.tsx", "x!y", "a\u0000b"]) {
      expect(validScopePath(p), p).toBe(false);
    }
    expect(validScopePath(null)).toBe(false);
    expect(validScopePath(undefined)).toBe(false);
  });
});

describe("actionRetryable", () => {
  const a = (status: ControlAction["status"]) => ({ status }) as ControlAction;
  it("chưa có lệnh hoặc lệnh hết hiệu lực/hết hạn/đã huỷ thì bấm lại được", () => {
    expect(actionRetryable(null)).toBe(true);
    for (const s of ["STALE", "EXPIRED", "CANCELLED"] as const) expect(actionRetryable(a(s))).toBe(true);
    for (const s of ["PENDING", "APPLIED"] as const) expect(actionRetryable(a(s))).toBe(false);
  });
});
