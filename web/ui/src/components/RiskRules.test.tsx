import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RiskRules } from "./RiskRules";
import type { Policy } from "../risk";

const v2: Policy = {
  schema: 2, version: 3, preset: null, levels: ["low", "high"], default: "low",
  rules: [{ id: "G1", describe: "Có dòng tiền đi qua", level: "high", enabled: true }],
  classification: { devLower: "reason" }, requirements: {},
};

const html = (p: Policy) => renderToStaticMarkup(<RiskRules policy={p} base={p} presetRuleIds={[]} onChange={() => {}} />);

test("RiskRules schema 2: cột mô tả, không còn loại/mẫu khớp/agent", () => {
  const out = html(v2);
  expect(out).toContain("Mô tả");
  expect(out).toContain("Có dòng tiền đi qua");
  expect(out).not.toContain("Mẫu khớp");
  expect(out).not.toContain("Đánh giá tác động của agent");
  expect(out).toContain("Dev hạ mức");
});

test("RiskRules schema 1: chỉ đọc, hiện luật cũ, không có nút sửa", () => {
  const v1: Policy = { ...v2, schema: 1, classification: { agent: "raise", devLower: "reason" },
    rules: [{ id: "G1", kind: "label", match: ["money-movement"], level: "high", note: "Có dòng tiền", enabled: true }] };
  const out = html(v1);
  expect(out).toContain("money-movement");
  expect(out).toContain("schema 1");
  expect(out).not.toContain(">Sửa<");
  expect(out).not.toContain("Thêm rule");
});
