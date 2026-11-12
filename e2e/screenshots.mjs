// Chụp 5 trang web: bảng (/), tính năng, live, cổng DESIGN và CASES (?g=). Env: WEB_URL, WEB_TOKEN, SHOTS, REPO_ID, KEY. Đăng nhập bằng POST /api/login để có cookie phiên.
import { chromium } from "playwright";

const { WEB_URL, WEB_TOKEN, SHOTS, REPO_ID, KEY } = process.env;
const pages = [
  ["board", "/"],
  ["feature", `/f/${REPO_ID}/${KEY}`],
  ["live", `/f/${REPO_ID}/${KEY}/live`],
  ["feature-design", `/f/${REPO_ID}/${KEY}?g=DESIGN`],
  ["feature-cases", `/f/${REPO_ID}/${KEY}?g=CASES`],
];

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: "vi-VN" });
  const tall = await browser.newContext({ viewport: { width: 1440, height: 1400 }, locale: "vi-VN" }); // cột phải cổng DESIGN/CASES đủ chỗ cho giả định
  const res = await ctx.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
  if (!res.ok()) throw new Error(`đăng nhập thất bại: HTTP ${res.status()}`);
  const res2 = await tall.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
  if (!res2.ok()) throw new Error(`đăng nhập thất bại: HTTP ${res2.status()}`);
  for (const [name, path] of pages) {
    const page = await (name.startsWith("feature-") ? tall : ctx).newPage();
    await page.goto(`${WEB_URL}${path}`, { waitUntil: "load" }); // SSE /api/stream giữ kết nối nên không có networkidle
    if (name === "feature-design") await page.waitForSelector("svg[role=img]", { timeout: 10000 });
    await page.waitForTimeout(2000);
    await page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true });
    console.log(`đã chụp ${name}.png`);
    await page.close();
  }
} finally {
  await browser.close();
}
