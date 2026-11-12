// Chính sách rủi ro trên web như dev thật: trang /risk/<repo>#rules có rule (schema 2, mô tả bằng lời) của fixture và
// không còn tab "Thử phân loại"; trang tính năng có panel "Rủi ro" với mức mong đợi và khối "Xếp loại LLM" (kết quả
// subagent aisdlc-risk-classifier: mức, rule khớp kèm câu trích). Đăng nhập bằng token login (POST /api/login, cookie
// phiên).
// Env: WEB_URL, WEB_TOKEN, REPO_ID, KEY,
//   RULE (id rule), DESCRIBE (một đoạn mô tả của rule), LEVEL (mức trên panel), GATE (cổng có review card mang risk),
//   LLM_GATE (cổng có khối Xếp loại LLM: DESIGN, luồng nhẹ CASES), EVIDENCE (một đoạn câu trích của RULE),
//   SHOTS (tùy chọn, thư mục): chụp risk-rules.png, risk-levels.png, risk-feature.png.
// Không in token.
import { chromium } from "playwright";

const { WEB_URL, WEB_TOKEN, REPO_ID, KEY, RULE, DESCRIBE, LEVEL, GATE, LLM_GATE, EVIDENCE, SHOTS } = process.env;

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1400 } });
  const res = await ctx.request.post(`${WEB_URL}/api/login`, { data: { token: WEB_TOKEN } });
  if (!res.ok()) throw new Error(`đăng nhập thất bại: HTTP ${res.status()}`);
  const page = await ctx.newPage();
  const noError = async () => {
    const err = page.getByRole("alert");
    if (await err.count()) throw new Error(`trang báo lỗi: ${await err.first().innerText()}`);
  };
  // Chờ locator xuất hiện; chưa có thì tải lại trang (event có thể tới sau khi trang mở), tối đa 20 giây.
  const waitFor = async (url, locate) => {
    const until = Date.now() + 20000;
    for (;;) {
      await page.goto(url, { waitUntil: "load" });
      try {
        await locate().first().waitFor({ timeout: 3000 });
        return;
      } catch (e) {
        if (Date.now() > until) throw e;
      }
    }
  };

  // Tab Rule nhận diện: dòng có id rule và mô tả của fixture; không còn tab thử phân loại (server không có LLM).
  const riskPath = `/risk/${REPO_ID}`;
  await waitFor(`${WEB_URL}${riskPath}#rules`, () =>
    page.locator("table.grid tbody tr").filter({ hasText: RULE }).filter({ hasText: DESCRIBE }));
  await noError();
  console.log(`thấy rule ${RULE} (${DESCRIBE}) ở ${riskPath}#rules`);
  const tabs = page.getByRole("navigation", { name: "Chính sách rủi ro" });
  if (await tabs.getByText(/thử phân loại/i).count()) throw new Error("trang chính sách còn tab Thử phân loại");
  if (SHOTS) {
    await page.waitForTimeout(500);
    await page.screenshot({ path: `${SHOTS}/risk-rules.png`, fullPage: true });
    await page.getByRole("navigation", { name: "Chính sách rủi ro" }).getByRole("link", { name: "Yêu cầu theo mức" }).click();
    await page.waitForTimeout(1000);
    await page.screenshot({ path: `${SHOTS}/risk-levels.png`, fullPage: true });
  }

  // Trang tính năng, cổng GATE: panel "Rủi ro" có pill mức LEVEL.
  const panel = () => page.getByRole("region", { name: "Rủi ro" });
  await waitFor(`${WEB_URL}/f/${REPO_ID}/${KEY}?g=${GATE}`, () => panel().locator(".pill.risk", { hasText: LEVEL }));
  await noError();
  console.log(`panel Rủi ro của ${KEY} (${GATE}) có mức ${LEVEL}`);

  // Review card cổng LLM_GATE: khối "Xếp loại LLM" có mức LEVEL, rule RULE kèm câu trích EVIDENCE.
  const llm = () => page.locator("section.rk-llm").filter({ hasText: "Xếp loại LLM" });
  await waitFor(`${WEB_URL}/f/${REPO_ID}/${KEY}?g=${LLM_GATE}`, () =>
    llm().filter({ has: page.locator(".pill.risk", { hasText: LEVEL }) }).filter({ hasText: EVIDENCE }));
  await noError();
  if (!(await llm().locator("li").filter({ hasText: RULE }).filter({ hasText: EVIDENCE }).count())) {
    throw new Error(`khối Xếp loại LLM thiếu rule ${RULE} kèm câu trích`);
  }
  console.log(`khối Xếp loại LLM của ${KEY} (${LLM_GATE}): mức ${LEVEL}, ${RULE} kèm câu trích`);
  if (SHOTS) {
    await page.waitForTimeout(1000);
    await page.screenshot({ path: `${SHOTS}/risk-feature.png`, fullPage: true });
  }
} finally {
  await browser.close();
}
