// Thao tác trên trang B như dev thật (chế độ duyệt trên web). Đăng nhập bằng token login (POST /api/login, cookie phiên).
// Env: WEB_URL, WEB_TOKEN, REPO_ID, KEY, ACTION, và theo ACTION:
//   approve        GATE: mở ?g=GATE, bấm Duyệt → Xác nhận duyệt; SHOT (tùy chọn) chụp hộp xác nhận trước khi bấm.
//   preview        GATE, SHOT: mở hộp xác nhận, chụp, bấm Hủy (không duyệt).
//   confirm-import GATE: bấm "Xác nhận" ở dải nhập từ local của cổng.
//   shot           PAGE (đường dẫn), SHOT: chụp trang.
// SHOT là đường dẫn file PNG. Không in token.
import { chromium } from "playwright";

const { WEB_URL, WEB_TOKEN, REPO_ID, KEY, ACTION, GATE, SHOT, PAGE } = process.env;
const feature = `/f/${REPO_ID}/${KEY}`;

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1400 }, locale: "vi-VN" });
  const res = await ctx.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
  if (!res.ok()) throw new Error(`đăng nhập thất bại: HTTP ${res.status()}`);
  const page = await ctx.newPage();

  const openConfirm = async () => {
    await page.goto(`${WEB_URL}${feature}?g=${GATE}`, { waitUntil: "load" });
    const btn = page.getByRole("button", { name: /^Duyệt @/ });
    await btn.waitFor({ timeout: 20000 });
    await btn.click();
    const box = page.getByRole("group", { name: "Xác nhận duyệt" });
    await box.waitFor({ timeout: 5000 });
    return box;
  };
  const noError = async () => {
    const err = page.getByRole("alert");
    if (await err.count()) throw new Error(`trang báo lỗi: ${await err.first().innerText()}`);
  };

  switch (ACTION) {
    case "approve": {
      const box = await openConfirm();
      if (SHOT) await box.screenshot({ path: SHOT });
      await box.getByRole("button", { name: "Xác nhận duyệt" }).click();
      await box.waitFor({ state: "detached", timeout: 10000 }).catch(() => {});
      await noError();
      await page.getByRole("button", { name: /^Duyệt @/ }).waitFor({ state: "detached", timeout: 10000 });
      console.log(`đã duyệt ${KEY} ${GATE} trên web`);
      break;
    }
    case "preview": {
      const box = await openConfirm();
      await page.waitForTimeout(500);
      await page.screenshot({ path: SHOT, fullPage: false });
      await box.getByRole("button", { name: "Hủy" }).click();
      console.log(`đã chụp hộp xác nhận ${KEY} ${GATE}`);
      break;
    }
    case "confirm-import": {
      await page.goto(`${WEB_URL}${feature}`, { waitUntil: "load" });
      const strip = page.locator(".import-strip", { hasText: GATE });
      await strip.waitFor({ timeout: 20000 });
      if (SHOT) await page.screenshot({ path: SHOT, fullPage: false });
      await strip.getByRole("button", { name: "Xác nhận" }).click();
      await strip.waitFor({ state: "detached", timeout: 10000 });
      await noError();
      console.log(`đã xác nhận bản nhập ${KEY} ${GATE}`);
      break;
    }
    case "shot": {
      await page.goto(`${WEB_URL}${PAGE}`, { waitUntil: "load" });
      await page.waitForTimeout(2000);
      await page.screenshot({ path: SHOT, fullPage: true });
      console.log(`đã chụp ${PAGE}`);
      break;
    }
    default:
      throw new Error(`ACTION không hợp lệ: ${ACTION}`);
  }
} finally {
  await browser.close();
}
