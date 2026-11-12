import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RiskLLM } from "./RiskLLM";
import type { RiskLLMData } from "../types";

const base: RiskLLMData = {
  level: "high", summary: "Ghi tiền vào sổ",
  matched: [{ rule: "R1", level: "high", on: "“ghi số dư” (design.md)" }],
  notMatched: [{ rule: "R2", reason: "không đổi lược đồ" }],
};
const text = (d: RiskLLMData) => renderToStaticMarkup(<RiskLLM data={d} />).replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");

test("RiskLLM: mức, tóm tắt, rule khớp kèm trích dẫn, rule không khớp kèm lý do", () => {
  const out = text(base);
  expect(out).toContain("Xếp loại LLM");
  expect(out).toContain("high");
  expect(out).toContain("Ghi tiền vào sổ");
  expect(out).toContain("R1");
  expect(out).toContain("“ghi số dư” (design.md)");
  expect(out).toContain("R2");
  expect(out).toContain("không đổi lược đồ");
});

test("RiskLLM: override hiện mức rule và lý do; không override thì không có", () => {
  expect(text({ ...base, override: { from: "medium", reason: "luồng chỉ đọc" } })).toContain("Lệch mức rule (medium): luồng chỉ đọc");
  expect(text(base)).not.toContain("Lệch mức rule");
});

test("RiskLLM: không rule nào khớp thì ghi rõ", () => {
  expect(text({ ...base, matched: [] })).toContain("Không rule nào khớp");
});
