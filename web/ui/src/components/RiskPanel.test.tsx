import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { RiskPanel } from "./RiskPanel";
import type { ReviewCardData } from "../types";

/** Chữ hiển thị (bỏ thẻ HTML, thẻ dt/dd tách bằng ": "). */
function text(card: ReviewCardData): string {
  return renderToStaticMarkup(<MemoryRouter><RiskPanel card={card} repoId="local/x" /></MemoryRouter>)
    .replace(/<\/dt>/g, ": ").replace(/<[^>]+>/g, "");
}

test("RiskPanel: hiện đánh giá của agent (mức, lý do, nhãn)", () => {
  const out = text({ gate: "DESIGN", flags: [], risk: { level: "high", policyVersion: 2, matched: [],
    agent: { level: "medium", labels: ["schema-change", "auth"], reason: "Đổi bảng quyền" } } });
  expect(out).toContain("Agent: medium — Đổi bảng quyền, nhãn: schema-change, auth");
});

test("RiskPanel: agent không có nhãn thì bỏ phần nhãn; không có đánh giá thì ghi rõ", () => {
  const noLabels = text({ gate: "CASES", flags: [], risk: { level: "high", policyVersion: 2, matched: [],
    agent: { level: "high", labels: [], reason: "Ghi dữ liệu" } } });
  expect(noLabels).toContain("Agent: high — Ghi dữ liệu");
  expect(noLabels).not.toContain("nhãn:");
  const none = text({ gate: "DESIGN", flags: [], risk: { level: "high", policyVersion: 2, matched: [], agent: null } });
  expect(none).toContain("Agent: Không có đánh giá");
});
