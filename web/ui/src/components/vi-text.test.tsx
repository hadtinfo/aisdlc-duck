import { afterEach, expect, test, vi as vitest } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { resetSession } from "../api";
import { getLocale, setLocale, tIn } from "../i18n";
import { vi } from "../i18n/vi";
import type { Policy } from "../risk";
import { Assumptions } from "./Assumptions";
import { applyLocaleChoice } from "./LocaleSwitch";
import { RiskRequirements } from "./RiskRequirements";

// Ghim chữ tiếng Việt như trước khi tách chuỗi (locale vi phải ra đúng chữ cũ).
// Bỏ thẻ HTML lặp tới khi không đổi (một lượt thay thế có thể để lại thẻ ghép từ phần còn sót).
const stripTags = (html: string) => {
  let prev;
  do { prev = html; html = html.replace(/<[^>]*>/g, ""); } while (html !== prev);
  return html;
};
const text = (html: string) => stripTags(html);

afterEach(() => { setLocale("vi"); vitest.unstubAllGlobals(); resetSession(); });

test("Assumptions vi: 'Đã xem 0/2 giả định'", () => {
  const html = renderToStaticMarkup(
    <Assumptions items={[{ text: "A" }, { text: "B" }] as never} section={true} storageKey="k" />);
  expect(html).toContain("Đã xem <b>0/2</b> giả định");
  setLocale("en");
  expect(text(renderToStaticMarkup(
    <Assumptions items={[{ text: "A" }] as never} section={true} storageKey="k" />))).toContain("Reviewed 0/1 assumptions");
});

const policy: Policy = {
  schema: 2, version: 1, preset: null, levels: ["low", "high"], default: "low", rules: [],
  classification: { devLower: "reason" }, requirements: {},
};

test("RiskRequirements: có khoảng trắng giữa hai câu quy trình (vi và en)", () => {
  const vi1 = text(renderToStaticMarkup(<RiskRequirements policy={policy} base={policy} onChange={() => {}} />));
  expect(vi1).toContain("manual: dev duyệt tay. Chưa chọn thì là manual");
  setLocale("en");
  const en1 = text(renderToStaticMarkup(<RiskRequirements policy={policy} base={policy} onChange={() => {}} />));
  expect(en1).toContain("the dev approves manually. Unset means manual");
});

test("chữ vi giữ nguyên ở StepMain và tooltip lệch giờ của Review", () => {
  expect(vi["rvw.notPassed"]).toBe("chưa đạt: {list}");
  expect(vi["rvw.skewTitle"]).toBe("Đồng hồ máy gửi lệch");
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

test("applyLocaleChoice: chưa đăng nhập chỉ đổi giao diện, không gọi server", async () => {
  const fetchMock = vitest.fn();
  vitest.stubGlobal("fetch", fetchMock);
  expect(await applyLocaleChoice("en", "vi", true)).toBe("");
  expect(getLocale()).toBe("en");
  expect(fetchMock).not.toHaveBeenCalled();
});

test("applyLocaleChoice: đã đăng nhập → PUT; server lỗi thì trả về lựa chọn cũ, báo bằng ngôn ngữ cũ", async () => {
  const { login } = await import("../api");
  const me = { login: "dev", displayName: "Dev", role: "dev", csrf: "c", locale: null };
  let fail = false;
  const fetchMock = vitest.fn(async (path: string, _init?: RequestInit) => {
    if (path === "/api/login") return json(200, me);
    return fail ? json(500, {}) : json(200, { ...me, locale: "en" });
  });
  vitest.stubGlobal("fetch", fetchMock);
  await login("tok");
  expect(await applyLocaleChoice("en", "vi", true)).toBe("");
  expect(fetchMock.mock.calls[1][0]).toBe("/api/me/locale");
  expect(fetchMock.mock.calls[1][1]?.method).toBe("PUT");
  expect(getLocale()).toBe("en");
  fail = true;
  expect(await applyLocaleChoice("vi", "en", true)).toBe(tIn("en", "locale.saveFailed"));
  expect(getLocale()).toBe("en");
});
