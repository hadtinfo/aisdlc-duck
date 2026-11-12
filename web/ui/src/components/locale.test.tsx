import type { ReactElement } from "react";
import { afterEach, expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { setLocale } from "../i18n";
import { AgentReview } from "./AgentReview";
import { CopyCommand } from "./CopyCommand";
import { FlagList } from "./FlagList";
import { Inbox } from "./Inbox";
import { LocaleSwitch } from "./LocaleSwitch";
import { RiskLLM } from "./RiskLLM";

const VI_CHARS = /[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]/i;
const text = (el: ReactElement) => renderToStaticMarkup(<MemoryRouter>{el}</MemoryRouter>).replace(/<[^>]+>/g, " ");

afterEach(() => setLocale("vi"));

const all = () => [
  <Inbox items={[]} />,
  <Inbox items={[{ repoId: "r", key: "K-1", kind: "APPROVE", text: "Gate BRD is waiting for your approval", command: "aisdlc approve brd" }]} />,
  <CopyCommand command="aisdlc eval" />,
  <FlagList flags={[]} />,
  <AgentReview review={null} />,
  <RiskLLM data={{ level: "high", summary: "Money", matched: [], notMatched: [{ rule: "R2", reason: "read only" }] }} />,
  <LocaleSwitch />,
];

test("locale en: chữ giao diện không còn tiếng Việt", () => {
  setLocale("en");
  for (const el of all()) expect(VI_CHARS.test(text(el)), text(el)).toBe(false);
  expect(text(<Inbox items={[]} />)).toContain("Waiting for me");
});

test("locale vi giữ chữ cũ", () => {
  expect(text(<Inbox items={[]} />)).toContain("Việc chờ tôi");
  expect(text(<FlagList flags={[]} />)).toContain("Không có cờ");
});
