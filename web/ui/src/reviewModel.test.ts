/*
 * Nguồn fixture `fixtures/pay1-cards.json` (sinh 2026-11-03 từ CLI thật, không viết tay):
 *   1. `mvn -q -DskipTests -Dskip.ui package` → aisdlc-cli/target/aisdlc-cli.jar
 *   2. copy `e2e/fixture-service` ra thư mục tạm, `git init` + commit (đã có db/migration/V1__init.sql)
 *   3. `java -jar aisdlc-cli.jar setup --package com.example.fixture`, rồi `init PAY-1`
 *   4. copy `aisdlc-core/src/test/resources/review/pay1/*` (brd.md, design.md, design.yaml, cases.yaml,
 *      db/V2__daily_limit.sql) vào `aisdlc/features/PAY-1/`, commit
 *   5. `java -jar aisdlc-cli.jar review brd|design|cases --json`, ghép thành map "BRD"|"DESIGN"|"CASES" → thẻ.
 * DESIGN có SCOPE_OVERLAP (HIGH, S1/S2) và SCOPE_MATCHES_OLD_MIGRATION (WARN, V1__init.sql); test dưới dựa vào đó.
 */
import { afterEach, expect, test, vi } from "vitest";
import pay1Raw from "./fixtures/pay1-cards.json";
import type { FlagLevel, ReviewCardData, ReviewFlag, Snapshot, TablePart } from "./types";
import {
  cardFor, erLayout, flagTitle, flagTone, loadSeen, middleEllipsis, saveSeen, seenKey, sortFlags, stepMap,
  summaryTiles,
} from "./reviewModel";

const pay1 = pay1Raw as unknown as Record<"BRD" | "DESIGN" | "CASES", ReviewCardData>;
const brd = pay1.BRD;
const snap = { reviewCards: pay1 } as unknown as Snapshot;
const f = (code: string, level: FlagLevel): ReviewFlag => ({ code, level, message: code, refs: [] });

function cardWithManyDirs(n: number): ReviewCardData {
  const files = Array.from({ length: n }, (_, i) => `src/main/java/d${String(i).padStart(2, "0")}/A.java`);
  return {
    gate: "DESIGN", flags: [],
    design: {
      steps: [{ id: "S1", title: "nhiều", scope: ["src/**"], budget: { files: 1, lines: 1 },
        matchedCount: n, matchedFiles: files }],
      apiLines: [], statusCodes: [], apiRows: [], tables: [], touchesExisting: [],
    },
  };
}
const col = (name: string) => ({ name, type: "INT", constraints: [] });
const altered = (cols: string[], added: string[]): TablePart =>
  ({ name: "account", status: "ALTERED", columns: cols.map(col), addedColumns: added.map(col) });
const dropped = (name: string): TablePart => ({ name, status: "DROPPED", columns: [col("id")] });

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

test("cardFor: không có reviewCards hoặc thiếu cổng → null", () => {
  expect(cardFor({ ...snap, reviewCards: undefined }, "DESIGN")).toBeNull();
  expect(cardFor({ ...snap, reviewCards: { BRD: brd } }, "CASES")).toBeNull();
  expect(cardFor(snap, "DESIGN")).toBe(pay1.DESIGN);
});
test("flagTone theo mức", () => {
  expect(["HIGH","WARN","INFO","NA"].map((l) => flagTone(l as FlagLevel))).toEqual(["bad","warn","ok","idle"]);
});
test("flagTitle: nhãn tiếng Việt, *_FAILED/DOC_UNREADABLE, mặc định = code", () => {
  expect(flagTitle(f("SCOPE_OVERLAP", "HIGH"))).toBe("Chồng lấn scope");
  expect(flagTitle(f("CARD_FAILED", "NA"))).toBe("Không tính được");
  expect(flagTitle(f("DOC_UNREADABLE", "NA"))).toBe("Không tính được");
  expect(flagTitle(f("LẠ_QUÁ", "WARN"))).toBe("LẠ_QUÁ");
});
test("sortFlags: HIGH, WARN, INFO, NA; giữ thứ tự gốc trong cùng mức", () => {
  const s = sortFlags([f("A", "INFO"), f("B", "NA"), f("C", "WARN"), f("D", "HIGH"), f("E", "WARN")]);
  expect(s.map((x) => x.code)).toEqual(["D", "C", "E", "A", "B"]);
});
test("cùng code hai mức → hai cờ, đếm riêng", () => {
  const card = { gate: "DESIGN", flags: [f("DB_TOUCHES_EXISTING","HIGH"), f("DB_TOUCHES_EXISTING","WARN")] };
  const t = summaryTiles(card as ReviewCardData);
  expect(t.find((x) => x.label === "Cờ đỏ")!.value).toBe("1");
  expect(t.find((x) => x.label === "Cảnh báo")!.value).toBe("1");
});
test("summaryTiles DESIGN PAY-1 như mock", () => {
  const t = summaryTiles(pay1.DESIGN);
  expect(t.map((x) => x.label)).toEqual(["Bước", "Cờ đỏ", "Cảnh báo", "DB", "API"]);
  expect(t[0]).toMatchObject({ value: "2", sub: "S1, S2" });
  expect(t[3].value).toBe("+1 bảng");          // daily_limit NEW
  expect(t[3].sub).toBe("không sửa bảng cũ");    // touchesExisting rỗng
  expect(t[4].sub).toBe("200 · 400 · 404");
});
test("summaryTiles CASES và BRD PAY-1", () => {
  const c = summaryTiles(pay1.CASES);
  expect(c.map((x) => [x.label, x.value])).toEqual([
    ["Case", "6"], ["Cờ đỏ", "0"], ["Cảnh báo", "2"], ["Bước thiếu case", "0"], ["Quy tắc thiếu case", "—"],
  ]);
  const b = summaryTiles(pay1.BRD);
  expect(b.map((x) => [x.label, x.value])).toEqual([
    ["Quy tắc", "4"], ["Tiêu chí", "2"], ["Giả định", "không ghi"], ["Ngoài phạm vi", "2"],
  ]);
  expect(b[0].sub).toBe("0 có ID");
});
test("summaryTiles thẻ chỉ có cờ NA (thẻ lỗi) không ném", () => {
  expect(() => summaryTiles({ gate: "DESIGN", flags: [f("CARD_FAILED","NA")] } as ReviewCardData)).not.toThrow();
  const t = summaryTiles({ gate: "DESIGN", flags: [f("CARD_FAILED","NA")] } as ReviewCardData);
  expect(t.find((x) => x.label === "Bước")!.value).toBe("—");
});
test("stepMap PAY-1: vùng chồng lấn đỏ có cạnh từ S1 và S2; V1__init.sql vàng từ S2; V2 nháp xanh", () => {
  const m = stepMap(pay1.DESIGN)!;
  const bad = m.nodes.filter((n) => n.tone === "bad");
  expect(bad).toHaveLength(1);
  expect(m.edges.filter((e) => e.to === bad[0].id).map((e) => e.from).sort()).toEqual(["S1", "S2"]);
  expect(m.nodes.find((n) => n.tone === "warn")!.full).toMatch(/V1__init\.sql$/);
  expect(m.nodes.find((n) => n.tone === "ok")!.full).toMatch(/V2__daily_limit\.sql$/);
});
test("stepMap PAY-1: glob rỗng trùng vùng chồng lấn thì gộp; bố cục cố định", () => {
  const m = stepMap(pay1.DESIGN)!;
  const targets = m.nodes.filter((n) => n.tone !== "step");
  expect(targets.map((n) => n.tone)).toEqual(["bad", "warn", "ok", "idle"]);
  expect(new Set(targets.map((n) => n.full)).size).toBe(targets.length);
  expect(m.width).toBe(710);
  expect(m.height).toBe(4 * 56 + 3 * 16 + 20);
  const s1 = m.nodes.find((n) => n.id === "S1")!;
  expect(s1).toMatchObject({ x: 10, w: 200, h: 64 });
  expect(targets[0]).toMatchObject({ x: 420, w: 280, h: 56 });
  expect(targets[0].title.length).toBeLessThanOrEqual(34);
  expect(stepMap({ gate: "DESIGN", flags: [] })).toBeNull();
});
test("stepMap chặn 12 đích, đếm phần ẩn", () => {
  const m = stepMap(cardWithManyDirs(30))!;   // 30 thư mục khớp khác nhau
  expect(m.nodes.filter((n) => n.tone !== "step").length).toBe(12);
  expect(m.hidden).toBe(18);
});
test("stepMap: matchedCount > matchedFiles thì đích cuối của bước ghi '· +N file khác'", () => {
  const card = cardWithManyDirs(3);
  card.design!.steps[0].matchedCount = 8;
  const t = stepMap(card)!.nodes.filter((n) => n.tone !== "step");
  expect(t.map((n) => n.sub)).toEqual(["1 file đang có", "1 file đang có", "1 file đang có · +5 file khác"]);
  expect(t[0].full).toBe("src/main/java/d00/");
});
/** Thẻ DESIGN hai bước, một cờ SCOPE_OVERLAP S1/S2 với vùng `region`; S2 khớp `files`. */
function overlapCard(region: string, files: string[]): ReviewCardData {
  const step = (id: string, matched: string[]) =>
    ({ id, title: id, scope: ["src/**"], budget: { files: 1, lines: 1 }, matchedCount: matched.length, matchedFiles: matched });
  return {
    gate: "DESIGN",
    flags: [{ code: "SCOPE_OVERLAP", level: "HIGH", message: "", refs: ["S1", "S2", region] }],
    design: { steps: [step("S1", []), step("S2", files)], apiLines: [], statusCodes: [], apiRows: [], tables: [],
      touchesExisting: [] },
  };
}
const plainFulls = (c: ReviewCardData) => stepMap(c)!.nodes.filter((n) => n.tone === "plain").map((n) => n.full);
test("stepMap: file dưới limit/ nhưng ngoài limit/web/ vẫn vẽ thành thư mục thường", () => {
  const c = overlapCard("src/main/java/**/limit/web/**", [
    "src/main/java/com/acme/limit/LimitService.java", "src/main/java/com/acme/limit/web/LimitController.java"]);
  expect(plainFulls(c)).toEqual(["src/main/java/com/acme/limit/"]);
});
test("stepMap: vùng **/web/** chỉ phủ file có đoạn web", () => {
  const c = overlapCard("**/web/**", ["a/web/X.java", "web/Y.java", "a/webby/Z.java", "b/Q.java"]);
  expect(plainFulls(c)).toEqual(["a/webby/", "b/"]);
});
test("stepMap: glob vùng — * và ? không vượt qua dấu /", () => {
  const c = overlapCard("src/*/W?.java", ["src/a/W1.java", "src/a/b/W1.java", "src/a/W12.java"]);
  expect(plainFulls(c)).toEqual(["src/a/b/", "src/a/"]);
});
test("stepMap: vùng là file cụ thể chỉ phủ đúng file đó", () => {
  const c = overlapCard("src/a/X.java", ["src/a/X.java", "src/a/X.javax", "src/a/Y.java"]);
  expect(plainFulls(c)).toEqual(["src/a/"]);
  expect(stepMap(c)!.nodes.find((n) => n.full === "src/a/")!.sub).toBe("2 file đang có");
});
test("stepMap: '+N file khác' chỉ gắn một lần vào đích thư mục của bước; không có thì bỏ", () => {
  const c = overlapCard("src/a/X.java", ["src/a/X.java", "src/b/Y.java", "src/c/Z.java"]);
  c.design!.steps[1].matchedCount = 8;
  c.design!.steps[1].plannedFiles = ["src/db/V9__x.sql"];
  expect(stepMap(c)!.nodes.filter((n) => n.sub.includes("file khác")).map((n) => [n.full, n.sub]))
    .toEqual([["src/c/", "1 file đang có · +5 file khác"]]);
  const only = overlapCard("src/a/X.java", ["src/a/X.java"]);   // chỉ có đích đỏ
  only.design!.steps[1].matchedCount = 9;
  expect(stepMap(only)!.nodes.some((n) => n.sub.includes("file khác"))).toBe(false);
});
test("stepMap/summaryTiles: thẻ DESIGN thiếu danh sách con không ném; bước không scope thì cạnh không nhãn", () => {
  const card = { gate: "DESIGN", design: { steps: [{ id: "S1", title: "t", budget: { files: 1, lines: 1 },
    matchedCount: 3, plannedFiles: ["a/V2.sql"] }] } } as unknown as ReviewCardData;
  expect(() => summaryTiles(card)).not.toThrow();
  expect(stepMap(card)!.edges).toEqual([{ from: "S1", to: "t0", tone: "plain" }]);
});
test("middleEllipsis giữ đầu và đuôi", () => {
  expect(middleEllipsis("src/main/java/com/acme/limit/web/LimitController.java", 30)).toHaveLength(30);
  expect(middleEllipsis("src/main/java/com/acme/limit/web/LimitController.java", 30)).toMatch(/^src\/.*….*Controller\.java$/);
  expect(middleEllipsis("ngắn", 30)).toBe("ngắn");
});
test("erLayout: bảng có sẵn bên trái, bảng mới bên phải, FK nối từ dòng cột", () => {
  const er = erLayout(pay1.DESIGN.design!.tables)!;
  const acc = er.boxes.find((b) => b.name === "account")!, dl = er.boxes.find((b) => b.name === "daily_limit")!;
  expect(acc.x).toBeLessThan(dl.x);
  expect(dl).toMatchObject({ tone: "ok", badge: "+ mới" });
  expect(acc).toMatchObject({ tone: "plain", badge: "có sẵn" });
  expect(er.edges).toEqual([{ from: "daily_limit", fromRow: 0, to: "account" }]);
  expect(dl.rows[0]).toEqual({ name: "account_id", type: "VARCHAR(20) PK FK", added: false });
  expect(dl.h).toBe(40 + 26 * 3);
  expect(erLayout([])).toBeNull();
});
test("erLayout ALTERED: cam, cột thêm đánh dấu added; DROPPED đỏ", () => {
  const er = erLayout([altered(["id"], ["status"]), dropped("old_log")])!;
  expect(er.boxes[0]).toMatchObject({ tone: "warn", badge: "bị sửa" });
  expect(er.boxes[0].rows.map((r) => [r.name, r.added])).toEqual([["id", false], ["status", true]]);
  expect(er.boxes[1]).toMatchObject({ tone: "bad", badge: "bị xóa" });
});

/** Node 20 không có Web Storage: dựng Storage tối thiểu để spy được như trong trình duyệt. */
class MemStorage {
  private m = new Map<string, string>();
  getItem(k: string) { return this.m.has(k) ? this.m.get(k)! : null; }
  setItem(k: string, v: string) { this.m.set(k, v); }
}
function stubStorage() {
  vi.stubGlobal("Storage", MemStorage);
  vi.stubGlobal("localStorage", new MemStorage());
}

test("seenKey gồm hash; loadSeen khi localStorage ném → []", () => {
  stubStorage();
  expect(seenKey("r", "PAY-1", "DESIGN", "abc")).not.toBe(seenKey("r", "PAY-1", "DESIGN", "abd"));
  expect(seenKey("r", "PAY-1", "DESIGN", "abc")).toBe("aisdlc.seen:r:PAY-1:DESIGN:abc");
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("x"); });
  expect(loadSeen("k")).toEqual([]);
});
test("saveSeen/loadSeen: đọc lại mảng số; sai kiểu → []; setItem ném thì bỏ qua", () => {
  stubStorage();
  saveSeen("k", [0, 2]);
  expect(loadSeen("k")).toEqual([0, 2]);
  localStorage.setItem("k", '{"a":1}');
  expect(loadSeen("k")).toEqual([]);
  localStorage.setItem("k", '["x"]');
  expect(loadSeen("k")).toEqual([]);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("đầy"); });
  expect(() => saveSeen("k", [1])).not.toThrow();
  vi.unstubAllGlobals();
  expect(loadSeen("k")).toEqual([]);   // không có localStorage
});

test("summaryTiles DB: sửa và xóa đếm riêng, tone bad khi có xóa, warn khi chỉ có sửa", () => {
  const mk = (tables: TablePart[]) => summaryTiles({ gate: "DESIGN", flags: [],
    design: { steps: [], apiLines: [], statusCodes: [], apiRows: [], tables, touchesExisting: [] } } as ReviewCardData)[3];
  const both = mk([altered(["id"], ["x"]), dropped("old")]);
  expect(both.value).toBe("+0 bảng · sửa 1 · xóa 1");
  expect(both.tone).toBe("bad");
  const onlyAlt = mk([altered(["id"], ["x"])]);
  expect(onlyAlt.value).toBe("+0 bảng · sửa 1");
  expect(onlyAlt.tone).toBe("warn");
  expect(mk([]).tone).toBeUndefined();
});
test("summaryTiles thẻ thiếu rows/coverage/outOfScope không ném", () => {
  expect(() => summaryTiles({ gate: "CASES", flags: [], cases: {} } as unknown as ReviewCardData)).not.toThrow();
  expect(summaryTiles({ gate: "CASES", flags: [], cases: {} } as unknown as ReviewCardData)[0].value).toBe("0");
  expect(() => summaryTiles({ gate: "BRD", flags: [], brd: { rules: 1, rulesWithId: 0, acceptanceCriteria: 0 } } as unknown as ReviewCardData)).not.toThrow();
});
test("stepMap: nhãn cạnh chỉ khi bước có đúng một glob", () => {
  const card = cardWithManyDirs(1);
  card.design!.steps[0].scope = ["src/**"];
  expect(stepMap(card)!.edges[0].label).toBe("src/**");
  card.design!.steps[0].scope = ["src/**", "db/**"];
  expect(stepMap(card)!.edges[0].label).toBeUndefined();
});
test("erLayout: tham chiếu có schema `public.account(id)` vẫn nối, không phân biệt hoa thường", () => {
  const t: TablePart[] = [
    { name: "account", status: "EXISTING", columns: [col("id")] },
    { name: "daily", status: "NEW", columns: [{ name: "account_id", type: "INT", constraints: ["FK"], references: "public.Account(id)" }] },
  ];
  expect(erLayout(t)!.edges).toEqual([{ from: "daily", fromRow: 0, to: "account" }]);
});

test("summaryTiles DB: có file SQL mà không đọc ra bảng thì ghi ? và cảnh báo, không ghi +0", () => {
  const card = { gate: "DESIGN", flags: [], design: { steps: [], tables: [], touchesExisting: [], apiLines: [], statusCodes: [] } } as unknown as ReviewCardData;
  const db = summaryTiles(card, 2)[3];
  expect(db.value).toBe("?");
  expect(db.tone).toBe("warn");
  expect(summaryTiles(card, 0)[3].value).toBe("+0 bảng");
});
