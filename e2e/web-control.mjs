// Điều khiển agent trên web như dev thật (2b-2): màn C (/f/<repo>/<key>/live) và trang B (/f/<repo>/<key>?g=…).
// Đăng nhập bằng token login (POST /api/login, cookie phiên; nút trên trang tự gửi CSRF).
// Env: WEB_URL, WEB_TOKEN, REPO_ID, KEY, ACTION, và theo ACTION:
//   reject     GATE, TEXT: trang B ?g=GATE, bấm Trả lại, nhập lý do, bấm Trả lại.
//   pause      màn C: bấm Tạm dừng, chờ "Đang tạm dừng bởi".
//   resume     màn C: bấm Tiếp tục, chờ nút Tạm dừng.
//   instruct   TEXT: màn C: nhập chỉ dẫn, bấm Gửi, chờ chỉ dẫn hiện "chờ giao".
//   accept     STEP: trang B ?g=STEP, bấm "Accept STEP" → "Xác nhận accept", chờ "Đã accept, chờ áp dụng".
//   allow      STEP, FILE: trang B ?g=STEP, bấm "Cho phép" ở dòng FILE, chờ "Đã cho phép, chờ áp dụng".
//   expect     PAGE (live | gate), GATE (với gate), TEXT: chờ chữ TEXT (tải lại trang, tối đa 20 giây).
// SHOT (tùy chọn, đường dẫn PNG): accept/allow chụp trước khi bấm (thấy nút), các ACTION khác chụp sau khi xong.
// Không in token.
import { chromium } from "playwright";

const { WEB_URL, WEB_TOKEN, REPO_ID, KEY, ACTION, GATE, STEP, FILE, TEXT, PAGE, SHOT } = process.env;
const feature = `${WEB_URL}/f/${REPO_ID}/${KEY}`;
const live = `${feature}/live`;

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1400 }, locale: "vi-VN" });
  const res = await ctx.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
  if (!res.ok()) throw new Error(`đăng nhập thất bại: HTTP ${res.status()}`);
  const page = await ctx.newPage();

  const noError = async (scope = page) => {
    const err = scope.getByRole("alert");
    if (await err.count()) throw new Error(`trang báo lỗi: ${await err.first().innerText()}`);
  };
  const shot = async () => { if (SHOT) await page.screenshot({ path: SHOT, fullPage: true }); };
  const panel = () => page.getByRole("region", { name: "Điều khiển agent" });
  // Chờ chữ xuất hiện; trang không tự làm mới thì tải lại (event SSE có thể tới trước khi trang mở).
  const waitText = async (url, text, scope = () => page) => {
    const until = Date.now() + 20000;
    for (;;) {
      await page.goto(url, { waitUntil: "load" });
      try {
        await scope().getByText(text).first().waitFor({ timeout: 3000 });
        return;
      } catch (e) {
        if (Date.now() > until) throw new Error(`không thấy "${text}" ở ${url}`);
      }
    }
  };

  switch (ACTION) {
    case "reject": {
      await page.goto(`${feature}?g=${GATE}`, { waitUntil: "load" });
      await page.getByRole("button", { name: "Trả lại" }).click({ timeout: 20000 });
      await page.locator("#ga-comment").fill(TEXT);
      await page.locator("form.ga-confirm").getByRole("button", { name: "Trả lại" }).click();
      await page.locator("form.ga-confirm").waitFor({ state: "detached", timeout: 10000 });
      await noError();
      await page.getByRole("heading", { name: "Bị trả lại" }).waitFor({ timeout: 10000 });
      await shot();
      console.log(`đã trả lại ${KEY} ${GATE} trên web`);
      break;
    }
    case "pause":
    case "resume": {
      const [from, to] = ACTION === "pause" ? ["Tạm dừng", "Tiếp tục"] : ["Tiếp tục", "Tạm dừng"];
      await page.goto(live, { waitUntil: "load" });
      const btn = panel().getByRole("button", { name: from, exact: true });
      await btn.waitFor({ timeout: 20000 });
      await page.waitForFunction((b) => !b.disabled, await btn.elementHandle(), { timeout: 10000 });
      await btn.click();
      await panel().getByRole("button", { name: to, exact: true }).waitFor({ timeout: 10000 });
      await noError(panel());
      if (ACTION === "pause") await panel().getByText("Đang tạm dừng bởi").waitFor({ timeout: 10000 });
      await shot();
      console.log(`đã bấm ${from} trên màn C`);
      break;
    }
    case "instruct": {
      await page.goto(live, { waitUntil: "load" });
      await page.locator("#ctl-text").fill(TEXT, { timeout: 20000 });
      await panel().getByRole("button", { name: "Gửi", exact: true }).click();
      const item = panel().locator(".ctl-list li", { hasText: TEXT });
      await item.waitFor({ timeout: 10000 });
      await item.getByText("chờ giao").waitFor({ timeout: 10000 });
      await noError(panel());
      await shot();
      console.log("đã gửi chỉ dẫn trên màn C");
      break;
    }
    case "accept": {
      await page.goto(`${feature}?g=${STEP}`, { waitUntil: "load" });
      const btn = page.getByRole("button", { name: `Accept ${STEP}`, exact: true });
      await btn.waitFor({ timeout: 20000 });
      if (SHOT) await page.screenshot({ path: SHOT, fullPage: true });
      await btn.click();
      const box = page.getByRole("group", { name: "Xác nhận accept" });
      await box.getByRole("button", { name: "Xác nhận accept" }).click({ timeout: 5000 });
      await page.getByText("Đã accept, chờ áp dụng").waitFor({ timeout: 10000 });
      await noError();
      console.log(`đã bấm Accept ${STEP} trên trang B`);
      break;
    }
    case "allow": {
      await waitText(`${feature}?g=${STEP}`, FILE, () => page.locator(".step-actions"));
      const row = page.locator(".step-actions .ctl-list li", { hasText: FILE });
      const btn = row.getByRole("button", { name: "Cho phép", exact: true });
      await btn.waitFor({ timeout: 10000 });
      if (SHOT) await page.screenshot({ path: SHOT, fullPage: true });
      await btn.click();
      await row.getByText("Đã cho phép, chờ áp dụng").waitFor({ timeout: 10000 });
      await noError();
      console.log(`đã bấm Cho phép ${FILE} (${STEP}) trên trang B`);
      break;
    }
    case "expect": {
      const url = PAGE === "live" ? live : `${feature}?g=${GATE}`;
      await waitText(url, TEXT, PAGE === "live" ? panel : () => page);
      await page.waitForTimeout(500);
      await shot();
      console.log(`thấy "${TEXT}" ở ${PAGE === "live" ? "màn C" : `trang B ?g=${GATE}`}`);
      break;
    }
    default:
      throw new Error(`ACTION không hợp lệ: ${ACTION}`);
  }
} finally {
  await browser.close();
}
