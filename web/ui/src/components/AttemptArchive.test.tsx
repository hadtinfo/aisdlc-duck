import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { AttemptArchive } from "./AttemptArchive";
import type { AttemptData } from "../types";

const fixture: AttemptData[] = [{
  attempt: 1,
  rewind: { checkpoint: "brd", by: "dev.a", at: "2026-11-13T09:00:00Z" },
  gates: [{
    gate: "BRD",
    submissions: [{ hash: "abc123", at: "2026-11-12T09:00:00Z", by: "dev.a",
      artifacts: [{ path: "brd.md", content: "# BRD cũ <b>x</b>" }] }],
    feedback: [{ kind: "REJECTED", by: "lead.b", at: "2026-11-12T10:00:00Z", comment: "thiếu rule" }],
    approvals: [{ approver: "lead.b", at: "2026-11-12T11:00:00Z", revokedAt: "2026-11-13T09:00:00Z" }],
  }],
  steps: [{ id: "S1", status: "ACCEPTED", files: 3, lines: 40, casesPassed: 2, casesTotal: 2 }],
  reviewCard: { gate: "REVIEW", flags: [{ code: "X", level: "WARN", message: "cờ cũ", refs: [] }] },
}];

test("AttemptArchive: tiêu đề lần làm, mốc, nội dung artifact (escape), không có nút", () => {
  const html = renderToStaticMarkup(<AttemptArchive attempts={fixture} />);
  expect(html).toContain("Các lần làm trước");
  expect(html).toContain("Lần 1");
  expect(html).toContain("brd");
  expect(html).toContain("dev.a");
  expect(html).toContain("brd.md");
  expect(html).toContain("# BRD cũ &lt;b&gt;x&lt;/b&gt;");
  expect(html).toContain("thiếu rule");
  expect(html).toContain("abc123");
  expect(html).toContain("cờ cũ");
  expect(html).not.toContain("<button");
});

test("AttemptArchive: mảng rỗng không vẽ gì", () => {
  expect(renderToStaticMarkup(<AttemptArchive attempts={[]} />)).toBe("");
});
