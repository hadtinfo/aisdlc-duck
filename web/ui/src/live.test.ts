import { describe, expect, it } from "vitest";
import { LiveEvent, editsByStep, sessionState } from "./live";

const NOW = new Date("2026-11-03T10:00:00Z");
const minAgo = (m: number) => new Date(NOW.getTime() - m * 60_000).toISOString();
let seq = 0;
const ev = (type: string, m: number, extra: Partial<LiveEvent> = {}): LiveEvent =>
  ({ id: ++seq, type, at: minAgo(m), step: null, path: null, outOfScope: false, sender: "dev", detail: null, ...extra });

describe("sessionState", () => {
  it("SESSION_START 10 phút trước, chưa Stop -> ACTIVE", () => {
    expect(sessionState([ev("SESSION_START", 10)], NOW)).toBe("ACTIVE");
  });
  it("FILE_EDIT cuối cùng 31 phút trước -> STOPPED", () => {
    expect(sessionState([ev("FILE_EDIT", 31), ev("SESSION_START", 60)], NOW)).toBe("STOPPED");
  });
  it("có SESSION_STOP sau event cuối -> STOPPED", () => {
    expect(sessionState([ev("SESSION_STOP", 2), ev("FILE_EDIT", 5)], NOW)).toBe("STOPPED");
  });
  it("SESSION_START sau SESSION_STOP -> ACTIVE", () => {
    expect(sessionState([ev("SESSION_START", 1), ev("SESSION_STOP", 5)], NOW)).toBe("ACTIVE");
  });
  it("không có event phiên -> NONE", () => {
    expect(sessionState([], NOW)).toBe("NONE");
    expect(sessionState([ev("COMMAND", 1)], NOW)).toBe("NONE");
  });
});

describe("editsByStep", () => {
  it("sửa cùng path hai lần thì giữ lần mới nhất", () => {
    const m = editsByStep([
      ev("FILE_EDIT", 9, { step: "S1", path: "a.java" }),
      ev("FILE_EDIT", 2, { step: "S1", path: "a.java", outOfScope: true }),
      ev("FILE_EDIT", 5, { step: "S1", path: "b.java" }),
      ev("FILE_EDIT", 3, { step: "S2", path: "c.java" }),
    ]);
    expect(m.get("S1")).toEqual([
      { path: "a.java", at: minAgo(2), outOfScope: true },
      { path: "b.java", at: minAgo(5), outOfScope: false },
    ]);
    expect(m.get("S2")?.length).toBe(1);
  });
});
