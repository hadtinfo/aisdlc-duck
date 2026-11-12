// Web theo ngôn ngữ người dùng chọn (spec i18n, mục Kiểm thử). Đăng nhập bằng token login (POST /api/login, cookie
// phiên), trình duyệt vi-VN như các script khác:
//   1. chưa chọn → giao diện tiếng Việt;
//   2. bấm EN ở bộ chọn EN/VI → giao diện tiếng Anh ngay, server lưu users.locale = en (GET /api/me);
//   3. phiên mới (trình duyệt sạch, vẫn vi-VN) → tiếng Anh theo lựa chọn đã lưu;
//   4. bấm VI → đặt lại tiếng Việt (các phần E2E khác giữ kỳ vọng tiếng Việt); finally luôn PUT /api/me/locale vi.
// Env: WEB_URL, WEB_TOKEN. Không in token.
import { chromium } from "playwright";

const { WEB_URL, WEB_TOKEN } = process.env;

const browser = await chromium.launch();
try {
  const session = async () => {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "vi-VN" });
    const res = await ctx.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
    if (!res.ok()) throw new Error(`đăng nhập thất bại: HTTP ${res.status()}`);
    const page = await ctx.newPage();
    await page.goto(`${WEB_URL}/`, { waitUntil: "load" });
    return { ctx, page };
  };
  const serverLocale = async (ctx) => {
    const r = await ctx.request.get(`${WEB_URL}/api/me`);
    if (!r.ok()) throw new Error(`GET /api/me: HTTP ${r.status()}`);
    return (await r.json()).locale ?? null;
  };
  // Giao diện đúng ngôn ngữ: nút đăng xuất, nhãn bộ lọc của bảng (có cả khi bảng trống), html lang; không còn nút của ngôn ngữ kia.
  const expectUI = async (page, lang) => {
    const want = lang === "en"
      ? { logout: "Log out", hideOld: "Hide done more than 7 days ago" }
      : { logout: "Đăng xuất", hideOld: "Ẩn đã xong quá 7 ngày" };
    const other = lang === "en" ? "Đăng xuất" : "Log out";
    await page.getByRole("button", { name: want.logout, exact: true }).waitFor({ timeout: 15000 });
    await page.getByText(want.hideOld).first().waitFor({ timeout: 5000 });
    if (await page.getByRole("button", { name: other, exact: true }).count()) {
      throw new Error(`giao diện ${lang} vẫn còn nút "${other}"`);
    }
    const htmlLang = await page.evaluate(() => document.documentElement.lang);
    if (htmlLang !== lang) throw new Error(`html lang=${htmlLang}, muốn ${lang}`);
    console.log(`giao diện ${lang}: đúng`);
  };
  const choose = async (page, lang) => {
    const group = page.getByRole("group", { name: lang === "en" ? "Ngôn ngữ" : "Language" });
    await group.getByRole("button", { name: lang.toUpperCase(), exact: true }).click();
  };
  const waitServer = async (ctx, want) => {
    for (let i = 0; i < 50; i++) {
      if ((await serverLocale(ctx)) === want) return;
      await new Promise((r) => setTimeout(r, 100));
    }
    throw new Error(`users.locale không thành ${want} (đang là ${await serverLocale(ctx)})`);
  };

  const a = await session();
  const before = await serverLocale(a.ctx);
  if (before === "en") throw new Error("user đã ở en trước khi chọn");
  await expectUI(a.page, "vi");
  await choose(a.page, "en");
  await expectUI(a.page, "en");
  await waitServer(a.ctx, "en");
  await a.ctx.close();

  const b = await session();
  await expectUI(b.page, "en");
  await choose(b.page, "vi");
  await expectUI(b.page, "vi");
  await waitServer(b.ctx, "vi");
  await b.ctx.close();
  console.log("web-locale: EN theo lựa chọn của người dùng, đã đặt lại VI");
} finally {
  // Luôn đặt lại vi trên server (kể cả khi kiểm tra ở trên lỗi giữa chừng), để phần E2E chạy sau giữ kỳ vọng tiếng Việt.
  try {
    const ctx = await browser.newContext({ locale: "vi-VN" });
    const res = await ctx.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
    const csrf = res.ok() ? (await res.json()).csrf : null;
    const put = await ctx.request.put(`${WEB_URL}/api/me/locale`, {
      data: { locale: "vi" },
      headers: csrf ? { "X-CSRF-Token": csrf } : {},
    });
    if (!put.ok()) {
      console.error(`đặt lại users.locale = vi thất bại: HTTP ${put.status()}`);
      process.exitCode = 1;
    }
    await ctx.close();
  } catch (e) {
    console.error(`đặt lại users.locale = vi thất bại: ${e}`);
    process.exitCode = 1;
  }
  await browser.close();
}
