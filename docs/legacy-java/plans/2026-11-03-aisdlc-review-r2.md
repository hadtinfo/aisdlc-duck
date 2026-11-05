# Review nhanh R2 — trang B vẽ review card — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Trang B (web, chỉ xem) đọc `reviewCards` trong snapshot và vẽ dải tóm tắt, cờ đỏ, giả định có ô tick, bản đồ bước → vùng code, ER diff, bảng API, độ phủ case và ma trận quy tắc × case, đúng như mock trang B; kèm hai việc R1 để lại (cột thêm mới của bảng `ALTERED`, `refs` sắp tự nhiên).

**Architecture:** Web không tính lại gì: mọi số, cờ, màu đều suy ra từ `reviewCards` (hợp đồng JSON spec §4). Phần suy luận (xếp nút, chọn màu, gom file theo thư mục, ô tóm tắt) là hàm thuần trong `ui/src/reviewModel.ts`, có Vitest. Component React chỉ vẽ kết quả của các hàm đó thành SVG/bảng. Java chỉ đổi ở `aisdlc-core` (thêm trường, đổi thứ tự `refs`), backend web không đổi vì snapshot được lưu và trả nguyên văn.

**Tech Stack:** Java 17 (aisdlc-core, JUnit 5); React 18 + TypeScript 5.6 + Vite 5.4.21 + Vitest 2.1.9; SVG viết tay (không thêm thư viện vẽ); Playwright 1.49.1 cho ảnh E2E.

**Spec:** `docs/superpowers/specs/2026-11-03-aisdlc-review-design.md` (§4 hợp đồng JSON, §6 hình trên trang B, §10 R2, §11 kiểm thử). Mock: artifact "Review cổng PAY-1" (https://claude.ai/artifact/SWJoSd5XDRUozFdZcjwgjm).

## Global Constraints

- Không thêm dependency npm hay Maven mới. SVG viết tay, dùng biến CSS sẵn có (`--ok`, `--warn`, `--bad`, `--info`, `--muted`, `--surface`, `--surface-2`, `--border`, `--text`) để hợp cả sáng lẫn tối.
- Giữ `schemaVersion: 1`; mọi thay đổi JSON chỉ là **thêm** trường. Web phải chạy được với snapshot không có `reviewCards` (CLI cũ) và với thẻ thiếu từng phần.
- Hình chỉ để định hướng: không tính được thì hiện chữ "không tính được", không đoán (spec §2 phản mục tiêu).
- Trang B vẫn chỉ xem: không gọi API ghi nào. Ô tick giả định chỉ lưu `localStorage` (bọc try/catch), không gửi lên server.
- Chữ trên giao diện bằng tiếng Việt, cùng giọng với các trang 2a.
- Mức cờ → màu: `HIGH` đỏ (`bad`), `WARN` vàng (`warn`), `INFO` xanh (`ok`, cờ "an toàn"), `NA` xám (`idle`).
- Khóa của cờ là cặp (`code`, `level`), không phải `code` (React `key` cũng theo cặp này + chỉ số).
- `mvn -q verify` (gồm `npm test` và `tsc --noEmit` của UI) và `e2e/run-e2e.sh` (cả `WEB=0`) phải xanh sau mỗi task.

## Review Focus

1. **Snapshot không có `reviewCards`, hoặc cổng không có thẻ (cổng còn khóa, thẻ lỗi bị bỏ):** trang hiện "Chưa có review card cho cổng này" và vẫn hiện nội dung 2a như cũ, không trắng trang. → test `cardFor` trong Task 2, kiểm tay trong Task 3.
2. **Tên file, glob, tên bảng rất dài hoặc rất nhiều mục (repo thật 10k file, bước khớp > 20 file):** chữ trong SVG được rút gọn giữa (`…`) kèm `<title>` đầy đủ, số đích bị chặn ở 12 với dòng "còn N mục", SVG không tràn ngang trang (`overflow-x: auto`). → test `middleEllipsis`, `stepMap` cap trong Task 2.
3. **Thẻ có cờ NA `CARD_FAILED`/`*_FAILED` hoặc `design` thiếu `tables`/`apiRows`:** từng khối tự ẩn hoặc hiện "không tính được", cờ NA vẫn liệt kê. → test `summaryTiles` với thẻ chỉ có cờ trong Task 2.
4. **Cùng mã cờ ở hai mức (`DB_TOUCHES_EXISTING` HIGH và WARN):** hai dòng riêng, đếm vào hai ô "Cờ đỏ" và "Cảnh báo" riêng. → test trong Task 2.
5. **Đổi nội dung cổng sau khi đã tick giả định (nộp lại, hash đổi):** ô tick reset vì khóa lưu gồm `currentHash`; `localStorage` ném lỗi (chế độ riêng tư) thì vẫn tick được trong phiên. → test `seenKey` và `loadSeen` trong Task 2.

---

### Task 1: Core — cột thêm mới của bảng ALTERED, `refs` sắp tự nhiên

**Files:**
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/review/ReviewCard.java` (`TablePart`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/review/SqlFacts.java` (`parse` cho `ALTER_TABLE`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/review/ReviewCardBuilder.java` (`tables(...)`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/review/CaseFlags.java` (refs `CASE_VALUE_NOT_IN_BRD`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/review/ScopeFlags.java` (refs `SCOPE_OVERLAP`)
- Modify: `docs/superpowers/specs/2026-11-03-aisdlc-review-design.md` (§4 hợp đồng, dòng trạng thái)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/review/ReviewCardBuilderTest.java`, `SqlFactsTest.java`, `CaseFlagsTest.java`, `ScopeFlagsTest.java`

**Interfaces:**
- Consumes: `CaseFlags.NATURAL` (Comparator<String> đã có, S2 trước S10).
- Produces: `TablePart(String name, String status, List<SqlColumn> columns, List<SqlColumn> addedColumns)`. `addedColumns` chỉ khác null khi `status == "ALTERED"` và nháp có `ALTER TABLE … ADD [COLUMN]`; JSON bỏ khi null. `columns` giữ nguyên nghĩa (cột hiện có). Task 2 đọc trường này.

- [ ] **Step 1: Viết test hỏng**

```java
// SqlFactsTest
@Test void alterTableAddColumnFillsColumns() {
    var st = SqlFacts.parse("ALTER TABLE account ADD COLUMN status VARCHAR(10) NOT NULL;").get(0);
    assertEquals("ALTER_TABLE", st.kind());
    assertEquals(List.of("status"), st.columns().stream().map(SqlColumn::name).toList());
    assertEquals("VARCHAR(10)", st.columns().get(0).type());
    assertEquals(List.of("NOT NULL"), st.columns().get(0).constraints());
}

// ReviewCardBuilderTest — schema có account(id, owner_name) như V1__init.sql; nháp:
//   ALTER TABLE account ADD COLUMN status VARCHAR(10) NOT NULL;
//   ALTER TABLE account ADD COLUMN status VARCHAR(10);   -- trùng tên: bỏ
//   ALTER TABLE account ADD COLUMN note TEXT;
@Test void alteredTableListsAddedColumns() {
    TablePart t = /* design card */ .tables().get(0);
    assertEquals("ALTERED", t.status());
    assertEquals(List.of("id", "owner_name"), names(t.columns()));
    assertEquals(List.of("status", "note"), names(t.addedColumns()));
}
@Test void newTableHasNoAddedColumnsField() {
    // PAY-1: daily_limit NEW → addedColumns null; JSON của thẻ không chứa "addedColumns"
}

// CaseFlagsTest: case C2 và C10 cùng có số không có trong BRD
@Test void caseValueRefsInNaturalOrder() { assertEquals(List.of("C2", "C10"), flag.refs()); }

// ScopeFlagsTest: bước S2 và S10 chồng lấn trên một glob
@Test void overlapRefsPutStepsInNaturalOrder() {
    assertTrue(flag.refs().indexOf("S2") < flag.refs().indexOf("S10"));
}
```

- [ ] **Step 2: Chạy, thấy hỏng** — `mvn -q -pl aisdlc-core test -Dtest='SqlFactsTest,ReviewCardBuilderTest,CaseFlagsTest,ScopeFlagsTest'` → FAIL (không biên dịch được `addedColumns`, refs sai thứ tự).

- [ ] **Step 3: Làm**
  - `SqlFacts.parse`: câu `ALTER_TABLE` mang `columns = alterAdds(câu gốc)` (hàm đã có). Không đổi cờ `DB_TOUCHES_EXISTING`/`DB_COLUMN_UNPARSED`.
  - `ReviewCardBuilder.tables`: gộp `columns` của mọi câu `ALTER_TABLE` trên bảng đã có vào `addedColumns` theo thứ tự câu, bỏ cột trùng tên (không phân biệt hoa thường) với cột hiện có hoặc cột đã thêm. `TRUNCATE`/`CREATE` lại không thêm cột. `DROP` sau đó thì bảng thành `DROPPED` và `addedColumns` null.
  - `CaseFlags`: `TreeSet` của id case dùng `NATURAL`. `ScopeFlags` overlap: `refs.sort(CaseFlags.NATURAL)`.
  - Spec §4: ở `tables[].status` `ALTERED` thay "chưa gộp cột thêm mới" bằng "`columns` là cột hiện có; `addedColumns[]` (chỉ có ở `ALTERED`) là cột thêm bằng `ADD COLUMN`, theo thứ tự câu, bỏ trùng"; `SCOPE_OVERLAP` và `CASE_VALUE_NOT_IN_BRD` đổi "thứ tự chữ" thành "thứ tự tự nhiên". Dòng trạng thái đầu spec: "R1 đã xong; R2 đang làm, kế hoạch `docs/superpowers/plans/2026-11-03-aisdlc-review-r2.md`".

- [ ] **Step 4: Chạy lại** — lệnh Step 2 PASS; rồi `mvn -q verify -Dskip.ui` PASS.

- [ ] **Step 5: Commit** — `feat(review): cột thêm mới cho bảng ALTERED, refs sắp tự nhiên`

---

### Task 2: UI — kiểu dữ liệu và mô hình vẽ (hàm thuần)

**Files:**
- Modify: `aisdlc-web/ui/src/types.ts`
- Create: `aisdlc-web/ui/src/reviewModel.ts`
- Test: `aisdlc-web/ui/src/reviewModel.test.ts`
- Create: `aisdlc-web/ui/src/fixtures/pay1-cards.json` (map `"BRD"|"DESIGN"|"CASES"` → thẻ của PAY-1, ghép từ `aisdlc review <gate> --json` chạy trên một repo dựng từ `e2e/fixture-service` + `aisdlc-core/src/test/resources/review/pay1`; ghi trong đầu file test cách đã sinh. Task 1 không làm đổi thẻ PAY-1 vì PAY-1 không có `ALTER`)

**Interfaces:**
- Consumes: hợp đồng JSON spec §4 + `addedColumns` của Task 1.
- Produces (Task 3 dùng đúng tên này):

```ts
// types.ts
export type FlagLevel = "HIGH" | "WARN" | "INFO" | "NA";
export interface ReviewFlag { code: string; level: FlagLevel; message: string; hint?: string; refs: string[] }
export interface Assumption { text: string; source: string }
export interface SqlColumn { name: string; type?: string; constraints: string[]; references?: string }
export interface TablePart { name: string; status: "NEW" | "EXISTING" | "ALTERED" | "DROPPED"; columns: SqlColumn[]; addedColumns?: SqlColumn[] }
export interface StepPart { id: string; title: string; scope: string[]; budget: { files: number; lines: number };
  matchedCount: number; matchedFiles: string[]; plannedFiles?: string[]; emptyGlobs?: string[] }
export interface ApiRow { status: string; when: string; body: string }
export interface ReviewCardData {
  gate: GateName; flags: ReviewFlag[]; assumptions?: Assumption[]; assumptionsSection?: boolean;
  brd?: { rules: number; rulesWithId: number; acceptanceCriteria: number; outOfScope: string[]; answeredQuestions: string[] };
  design?: { steps: StepPart[]; apiLines: string[]; statusCodes: string[]; apiRows: ApiRow[];
    tables: TablePart[]; touchesExisting: { kind: string; table: string; text: string }[] };
  cases?: { rows: { id: string; step: string; kind: string; title: string; given: string; expect: string; rules?: string[] }[];
    coverage: Record<string, Record<string, number>>; ruleMatrix?: Record<string, Record<string, string[]>> };
}
// Snapshot thêm: reviewCards?: Partial<Record<GateName, ReviewCardData>>
```

```ts
// reviewModel.ts
export type Tone = "ok" | "warn" | "bad" | "idle" | "info";   // trùng Tone của GateList
export function cardFor(snap: Snapshot, gate: GateName): ReviewCardData | null;
export function flagTone(level: FlagLevel): Tone;            // HIGH→bad, WARN→warn, INFO→ok, NA→idle
export function flagTitle(f: ReviewFlag): string;            // nhãn ngắn tiếng Việt theo code, mặc định = code
export function sortFlags(flags: ReviewFlag[]): ReviewFlag[]; // HIGH, WARN, INFO, NA; giữ thứ tự gốc trong cùng mức
export function middleEllipsis(s: string, max: number): string;
export interface Tile { label: string; value: string; sub: string; tone?: Tone }
export function summaryTiles(card: ReviewCardData): Tile[];
export interface MapNode { id: string; x: number; y: number; w: number; h: number; tone: Tone | "step" | "plain";
  title: string; sub: string; full: string }
export interface MapEdge { from: string; to: string; tone: Tone | "plain"; label?: string }
export interface StepMap { nodes: MapNode[]; edges: MapEdge[]; width: number; height: number; hidden: number }
export function stepMap(card: ReviewCardData): StepMap | null; // null khi không có design.steps
export interface ErBox { name: string; x: number; y: number; w: number; h: number; tone: Tone | "plain"; badge: string;
  rows: { name: string; type: string; added: boolean }[] }
export interface ErLayout { boxes: ErBox[]; edges: { from: string; fromRow: number; to: string }[]; width: number; height: number }
export function erLayout(tables: TablePart[]): ErLayout | null; // null khi tables rỗng
export function seenKey(repoId: string, feature: string, gate: GateName, hash: string): string;
export function loadSeen(key: string): number[];               // try/catch, lỗi → []
export function saveSeen(key: string, seen: number[]): void;   // try/catch, lỗi → bỏ qua
```

- [ ] **Step 1: Viết test hỏng** trong `reviewModel.test.ts`, dùng `fixtures/pay1-cards.json`:

```ts
test("cardFor: không có reviewCards hoặc thiếu cổng → null", () => {
  expect(cardFor({ ...snap, reviewCards: undefined }, "DESIGN")).toBeNull();
  expect(cardFor({ ...snap, reviewCards: { BRD: brd } }, "CASES")).toBeNull();
});
test("flagTone theo mức", () => {
  expect(["HIGH","WARN","INFO","NA"].map((l) => flagTone(l as FlagLevel))).toEqual(["bad","warn","ok","idle"]);
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
test("summaryTiles thẻ chỉ có cờ NA (thẻ lỗi) không ném", () => {
  expect(() => summaryTiles({ gate: "DESIGN", flags: [f("CARD_FAILED","NA")] } as ReviewCardData)).not.toThrow();
});
test("stepMap PAY-1: vùng chồng lấn đỏ có cạnh từ S1 và S2; V1__init.sql vàng từ S2; V2 nháp xanh", () => {
  const m = stepMap(pay1.DESIGN)!;
  const bad = m.nodes.filter((n) => n.tone === "bad");
  expect(bad).toHaveLength(1);
  expect(m.edges.filter((e) => e.to === bad[0].id).map((e) => e.from).sort()).toEqual(["S1", "S2"]);
  expect(m.nodes.find((n) => n.tone === "warn")!.full).toMatch(/V1__init\.sql$/);
  expect(m.nodes.find((n) => n.tone === "ok")!.full).toMatch(/V2__daily_limit\.sql$/);
});
test("stepMap chặn 12 đích, đếm phần ẩn", () => {
  const m = stepMap(cardWithManyDirs(30))!;   // 30 thư mục khớp khác nhau
  expect(m.nodes.filter((n) => n.tone !== "step").length).toBe(12);
  expect(m.hidden).toBe(18);
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
});
test("erLayout ALTERED: cam, cột thêm đánh dấu added; DROPPED đỏ", () => {
  const er = erLayout([altered(["id"], ["status"]), dropped("old_log")])!;
  expect(er.boxes[0]).toMatchObject({ tone: "warn", badge: "bị sửa" });
  expect(er.boxes[0].rows.map((r) => [r.name, r.added])).toEqual([["id", false], ["status", true]]);
  expect(er.boxes[1]).toMatchObject({ tone: "bad", badge: "bị xóa" });
});
test("seenKey gồm hash; loadSeen khi localStorage ném → []", () => {
  expect(seenKey("r", "PAY-1", "DESIGN", "abc")).not.toBe(seenKey("r", "PAY-1", "DESIGN", "abd"));
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("x"); });
  expect(loadSeen("k")).toEqual([]);
});
```

- [ ] **Step 2: Chạy, thấy hỏng** — `cd aisdlc-web/ui && npx vitest run src/reviewModel.test.ts` → FAIL (module chưa có).

- [ ] **Step 3: Làm `reviewModel.ts`** theo chữ ký trên. Các quyết định:
  - **`summaryTiles`** theo cổng. DESIGN: Bước (số, id nối `, `), Cờ đỏ (số HIGH, `sub` = `flagTitle` của cờ HIGH đầu, rỗng thì "không có"), Cảnh báo (số WARN, tương tự), DB (`+n bảng` với n = số NEW, thêm `· sửa m` nếu có ALTERED/DROPPED; `sub` "không sửa bảng cũ" khi `touchesExisting` rỗng, ngược lại "đụng m bảng cũ"), API (`n endpoint` từ `apiLines.length`, `sub` = `statusCodes` nối ` · `). CASES: Case (số dòng), Cờ đỏ, Cảnh báo, Bước thiếu case (số bước có tổng 0 trong `coverage`), Quy tắc thiếu case (số dòng `ruleMatrix` mà cả ba loại rỗng, hoặc "—" khi không có ma trận). BRD: Quy tắc (`rules`, `sub` "`rulesWithId` có ID"), Tiêu chí, Giả định (số, hoặc "không ghi" khi 0), Ngoài phạm vi. REVIEW: Cờ đỏ, Cảnh báo. Phần nào thiếu dữ liệu thì ô đó có `value` "—".
  - **`flagTitle`**: `SCOPE_OVERLAP` "Chồng lấn scope", `SCOPE_TOO_WIDE` "Scope quá rộng", `SCOPE_MATCHES_OLD_MIGRATION` "Glob chạm migration cũ", `SCOPE_BAD_GLOB` "Glob không hợp lệ", `SCOPE_NO_SCOPE` "Bước không có scope", `BUDGET_HIGH` "Ngân sách cao", `DB_TOUCHES_EXISTING` "Đụng bảng có sẵn", `DB_CREATE_ONLY` "DB an toàn", `DB_UNRECOGNIZED_SQL` "SQL không nhận dạng được", `DB_COLUMN_UNPARSED` "Cột không đọc được", `CASE_VALUE_NOT_IN_BRD` "Số liệu không có trong BRD", `CASES_RULE_UNCOVERED` "Quy tắc chưa có case", `CASES_RULE_UNKNOWN` "Case trỏ quy tắc lạ", `BRD_RULE_NO_ID` "Quy tắc thiếu ID", `DEPENDENCY_ADDED` "Thêm dependency", `ASSUMPTIONS_EMPTY` "Không ghi giả định"; mã kết thúc `_FAILED` hoặc `DOC_UNREADABLE` → "Không tính được".
  - **`stepMap`** — đích (bên phải) theo thứ tự ưu tiên, mỗi đích một lần (trùng `full` thì gộp cạnh):
    1. Mỗi cờ `SCOPE_OVERLAP`: `refs` là id bước có trong `design.steps` + một vùng (phần tử còn lại). Đích tone `bad`, `sub` "n bước cùng chạm", cạnh `bad` từ từng bước.
    2. Mỗi cờ `SCOPE_MATCHES_OLD_MIGRATION`: `refs[0]` là bước, các phần tử sau là file. Đích tone `warn`, `sub` "file đã có, glob <bước> vẫn khớp", cạnh `warn`.
    3. `plannedFiles` của mỗi bước: tone `ok`, `sub` "mới, khớp SQL nháp", cạnh `plain`.
    4. `emptyGlobs` của mỗi bước: tone `idle`, `sub` "chưa có file · sẽ tạo mới", cạnh `plain`.
    5. `matchedFiles` còn lại (chưa nằm trong đích 1–3; đường dẫn khớp đích vùng ở bước 1 khi vùng là glob thì coi như đã nằm trong — dùng so khớp tiền tố trước `**`), gom theo thư mục cha: `full` = thư mục + `/`, `sub` = "k file đang có"; nếu `matchedCount > matchedFiles.length` thì thêm vào `sub` của đích cuối của bước đó "· +N file khác". Tone `plain`, cạnh `plain`.
    Giữ 12 đích đầu theo thứ tự trên; `hidden` = số còn lại. Nhãn cạnh: glob đầu của bước. Bố cục: bước x=10 w=200 h=64 cách 24; đích x=420 w=280 h=56 cách 16; `width` 710; `height` = max hai cột + 20; các bước được căn giữa theo chiều dọc so với cột đích. `title` của đích = `middleEllipsis(full, 34)`.
  - **`erLayout`**: cột trái là `EXISTING`, `ALTERED`, `DROPPED` (theo tên), cột phải là `NEW` (theo tên); hai cột x=20 và x=420, w=260; hộp cao 40 + 26 × số dòng, cách nhau 20. Dòng = `columns` rồi `addedColumns` (`added: true`); `type` = `type` + `constraints` nối khoảng trắng (constraints viết như JSON: `PK`, `NOT NULL`, `FK`, `DEFAULT 0`…). Badge: NEW "+ mới"/ok, EXISTING "có sẵn"/plain, ALTERED "bị sửa"/warn, DROPPED "bị xóa"/bad. Cạnh: mỗi cột có `references` mà bảng đích có trong `tables` → `{from, fromRow, to}`; tham chiếu tới bảng không có trong `tables` thì bỏ.
  - **`seenKey`** = `aisdlc.seen:${repoId}:${feature}:${gate}:${hash}`; `loadSeen` đọc JSON mảng số, sai kiểu → [].

- [ ] **Step 4: Chạy lại** — `npx vitest run` PASS; `npx tsc --noEmit` không lỗi.

- [ ] **Step 5: Commit** — `feat(web): mô hình vẽ review card (tóm tắt, bản đồ bước, ER diff)`

---

### Task 3: UI — component và bố trí trang B

**Files:**
- Create: `aisdlc-web/ui/src/components/ReviewSummary.tsx` (dải ô tóm tắt)
- Create: `aisdlc-web/ui/src/components/FlagList.tsx`
- Create: `aisdlc-web/ui/src/components/Assumptions.tsx`
- Create: `aisdlc-web/ui/src/components/StepMapView.tsx`
- Create: `aisdlc-web/ui/src/components/ErDiagram.tsx`
- Create: `aisdlc-web/ui/src/components/ApiTable.tsx`
- Create: `aisdlc-web/ui/src/components/CaseCoverage.tsx` (bảng độ phủ bước × loại + ma trận quy tắc × case)
- Modify: `aisdlc-web/ui/src/pages/Review.tsx` (`GateMain`, chọn mục từ `?g=`)
- Modify: `aisdlc-web/ui/src/components/GatePanel.tsx` (cờ đỏ và giả định ở đầu cột phải)
- Modify: `aisdlc-web/ui/src/review.css`
- Test: `aisdlc-web/ui/src/routes.test.ts` (nếu thêm hàm đọc `?g=`)

**Interfaces:**
- Consumes: mọi export của `reviewModel.ts` và kiểu ở `types.ts` (Task 2).
- Produces: URL `/f/<repo>/<key>?g=DESIGN` mở thẳng cổng hoặc bước đó (Task 4 dùng để chụp ảnh). Mục chọn không hợp lệ thì quay về mục mặc định như cũ. Bấm mục trong danh sách cổng cập nhật `?g=` bằng `replace`.

- [ ] **Step 1: Test hỏng cho phần thuần còn lại** — nếu tách hàm `selectionFromQuery(search: string, ids: string[]): string | null` vào `routes.ts`: test `?g=DESIGN` → `"DESIGN"`, `?g=XYZ` → null, không có → null. Chạy `npx vitest run src/routes.test.ts` → FAIL.

- [ ] **Step 2: Làm component** (vẽ thuần từ `reviewModel`, không tự tính lại):
  - `GateMain` cho mỗi cổng có thẻ: `ReviewSummary` ở đầu, rồi:
    - **DESIGN:** panel "Bước nào được chạm vào đâu" (`StepMapView`, nguồn "sinh từ design.yaml + cây file hiện tại", chú thích 3 màu như mock, dòng "còn N mục không vẽ" khi `hidden > 0`); panel "Thay đổi DB" (`ErDiagram`, nguồn liệt kê file `db/*.sql` trong `snap.artifacts`; dưới hình: có cờ `DB_CREATE_ONLY` thì ghi `message` của nó; có `touchesExisting` thì bảng `kind | table | text`); panel "API" (`ApiTable` từ `apiRows`, dưới là `apiLines` dạng code); rồi `<details>` "Tài liệu gốc design.md — gồm sơ đồ sequence do agent vẽ, chỉ để tham khảo" chứa `Artifact design.md` cũ; giữ "Các bước" (`StepsTable`), `db/*.sql` và `design.yaml` như 2a nhưng gói trong một `<details>` "File gốc".
    - **CASES:** `CaseCoverage` (bảng độ phủ: hàng là bước theo khóa `coverage`, cột happy/error/boundary, ô 0 tô `warn`; khóa `"?"` hiện "không có bước"; ma trận quy tắc × case nếu có `ruleMatrix`: ô rỗng tô `bad-bg` chữ "thiếu", ô có id case liệt kê id); rồi `CasesTable` cũ.
    - **BRD:** tóm tắt; `outOfScope` và `answeredQuestions` thành hai danh sách ngắn; rồi `brd.md` như cũ.
    - **REVIEW:** tóm tắt; rồi nội dung 2a như cũ.
    - **Không có thẻ:** dòng `muted` "Chưa có review card cho cổng này (cổng còn khóa hoặc CLI chưa gửi)", rồi nội dung 2a như cũ.
  - `StepMapView`/`ErDiagram`: `<svg viewBox="0 0 {width} {height}" role="img" aria-label=...>` trong `div.diagram` (`overflow-x: auto`, `min-width: 560px` cho svg); nút `rect` theo lớp `n-step`/`n-bad`/`n-warn`/`n-ok`/`n-idle`/`n-plain`, cạnh là đường cong bậc ba `path` lớp `e`/`e-bad`/`e-warn`; mỗi nút có `<title>{full}</title>`. Màu chỉ qua biến CSS.
  - `GatePanel` khi là cổng: trước khối "Cổng …" thêm `FlagList` (cờ sắp `sortFlags`, mỗi cờ: `flagTitle` in hoa nhỏ màu theo `flagTone`, `message`, `hint` màu muted, `refs` dạng `code` nhỏ; không cờ → "Không có cờ") và `Assumptions` khi thẻ có trường `assumptions` (ô tick + `source` dạng chữ nhỏ; dòng "Đã xem x/n giả định"; `assumptionsSection === false` → "Tài liệu chưa có mục Giả định"; `assumptions` rỗng và `assumptionsSection` true → "Agent không ghi giả định nào"). Tick lưu qua `seenKey(repoId, key, gate, gate.currentHash)`.
  - CSS: thêm vào `review.css` các lớp trên, dải tóm tắt dạng lưới `repeat(auto-fit, minmax(140px, 1fr))` như mock; dưới 900px các panel xếp một cột.

- [ ] **Step 3: Chạy** — `npx vitest run` và `npm run build` PASS; `mvn -q -pl aisdlc-web -am verify` PASS.

- [ ] **Step 4: Xem bằng mắt** — chạy `docker compose` hoặc `mvn -pl aisdlc-web spring-boot:run` cùng E2E đã gửi PAY-1 (hoặc gửi `fixtures` bằng `curl /api/ingest`), mở `/f/local/fixture-service/PAY-1?g=DESIGN`, chụp màn hình sáng và tối, so với mock. Ghi đường dẫn ảnh vào báo cáo.

- [ ] **Step 5: Commit** — `feat(web): trang B vẽ review card (cờ, giả định, bản đồ bước, ER, API, độ phủ)`

---

### Task 4: E2E — kiểm `reviewCards` tới web và chụp trang B cổng DESIGN

**Files:**
- Modify: `e2e/web-check.sh` (hoặc chỗ kiểm web hiện có trong `e2e/run-e2e.sh`)
- Modify: `e2e/screenshots.mjs`
- Modify: `docs/README.vi.md` (mục trang B: một đoạn ngắn về review card trên web)

**Interfaces:**
- Consumes: `?g=` của Task 3; API `GET /api/features/<repo>/<key>` trả snapshot nguyên văn.

- [ ] **Step 1: Thêm kiểm** — sau khi PAY-1 đã có thẻ DESIGN (ngay sau `submit design`) và lúc xong: dùng `web_assert` có sẵn trong `e2e/web-check.sh` trên API chi tiết tính năng, biểu thức `jq` kiểm rằng `snapshot.reviewCards.DESIGN.flags` có `SCOPE_OVERLAP` mức `HIGH` và `SCOPE_MATCHES_OLD_MIGRATION` mức `WARN`, `snapshot.reviewCards.DESIGN.design.tables` có `daily_limit` `NEW`. Với `WEB=0` bước này bị bỏ như các kiểm web khác.
- [ ] **Step 2: Ảnh** — `screenshots.mjs` thêm trang `["feature-design", \`/f/${REPO_ID}/${KEY}?g=DESIGN\`]` và `["feature-cases", \`/f/${REPO_ID}/${KEY}?g=CASES\`]`; trước khi chụp đợi `page.waitForSelector("svg[role=img]")` cho trang design (hết 10 s thì fail rõ).
- [ ] **Step 3: Chạy** — `e2e/run-e2e.sh` PASS; `WEB=0 e2e/run-e2e.sh` PASS; `SCREENSHOTS=1 e2e/run-e2e.sh` PASS và in đường dẫn 5 ảnh. Mở `feature-design.png` xem có bản đồ bước (một ô đỏ, một ô vàng, một ô xanh) và ER diff.
- [ ] **Step 4: README** — trong mục trang B của `docs/README.vi.md` thêm: trang hiện review card do CLI tính (cờ, giả định, bản đồ bước, ER diff, độ phủ case); tick giả định chỉ lưu trên trình duyệt.
- [ ] **Step 5: Commit** — `test(e2e): kiểm reviewCards trên web, chụp trang B cổng DESIGN/CASES`

---

## Thứ tự và song song

- Task 1 (Java core) và Task 2 (UI) không chung file: chạy song song trên hai worktree. Task 2 viết fixture theo hợp đồng có `addedColumns` dù Task 1 chưa xong.
- Task 3 sau Task 2. Task 4 sau Task 1 và Task 3.
