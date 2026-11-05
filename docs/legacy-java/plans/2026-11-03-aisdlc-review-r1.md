# Review nhanh R1 (CLI) — Kế hoạch triển khai

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Trước khi duyệt mỗi cổng, dev thấy một review card sinh tất định (cờ đỏ, giả định, khung bước/DB/case) ở terminal, và snapshot gửi web mang theo card đó.

**Architecture:**
- Gói mới `com.aisdlc.core.review` trong `aisdlc-core`:
  - đọc thêm thông tin từ `brd.md`, `design.md`, `cases.yaml`;
  - phân tích nhẹ cây git (danh sách file), SQL nháp và `pom.xml`;
  - dựng `ReviewCard` và `ReviewFlag`, không phải `Finding`, không qua LLM.
- `aisdlc-cli` thêm lệnh `aisdlc review <gate>`, và cho `approve` in thẻ trước bước xác nhận.
- `SnapshotBuilder` thêm trường tùy chọn `reviewCards`, vẫn giữ `schemaVersion: 1`.

**Tech Stack:** Java 17, Jackson, picocli, JUnit 5/AssertJ (như giai đoạn 1). Không thêm dependency.

**Spec:** `docs/superpowers/specs/2026-11-03-aisdlc-review-design.md` (§3, §4, §5, §10 R1, §11). Spec gốc: `2026-11-02-aisdlc-design.md`.

## Global Constraints

- **Cờ đỏ không chặn cổng:**
  - Cờ là `ReviewFlag(code, level, message, hint, refs)` với `level ∈ WARN | INFO | NA`.
  - Cờ **không** vào `Finding` và **không** vào submission.
  - `GateService`, `BrdValidator`, `DesignValidator`, `CasesValidator` giữ nguyên hành vi. Mọi test cũ phải pass.
- **Mọi thứ tất định:** cùng đầu vào thì ra cùng JSON. Danh sách được sắp xếp ổn định (theo id bước, đường dẫn, mã cờ).
- **Không tính được thì báo NA, không đoán:** dùng `level: NA` kèm lý do.
- **Danh sách file** lấy từ git (`git ls-files --cached --others --exclude-standard`), không duyệt thư mục làm việc.
- **Hiệu năng:** `aisdlc review <gate>` và phần in thẻ trong `approve` dưới 1 giây trên PAY-1, không chạy Maven.
- **Thẻ dạng chữ:** rộng tối đa 100 cột, không mã màu khi stdout không phải TTY, đọc xong trong khoảng 30 giây. Mỗi danh sách dài cắt còn 10 dòng kèm dòng "… và N nữa".
- **`aisdlc review <gate>`:**
  - Agent được chạy, chỉ đọc, không cần console.
  - **Không** thêm `review` vào danh sách lệnh chỉ-dev trong `BashHeuristics`.
- **Snapshot:** giữ `schemaVersion: 1`. `reviewCards` là trường tùy chọn, dạng map `"BRD"|"DESIGN"|"CASES"|"REVIEW"` → card. Lỗi khi dựng card của một cổng thì bỏ card đó, không làm hỏng snapshot.
- **Mức cờ:** `HIGH` (đỏ: `SCOPE_OVERLAP`; `DB_TOUCHES_EXISTING` với DROP/TRUNCATE hoặc UPDATE/DELETE không có WHERE), `WARN` (vàng), `INFO` (xanh, "an toàn"), `NA`. Không mức nào chặn cổng. Thẻ chữ liệt kê theo thứ tự HIGH → WARN → NA → INFO.
- **Mã cờ và ngưỡng mặc định** (trong `aisdlc/config.yaml`, mục `review:`):

  | Mã / khóa | Giá trị mặc định |
  |---|---|
  | `SCOPE_TOO_WIDE` | `review.scopeMaxFiles: 30`; glob đúng bằng `**` hoặc `src/**` |
  | `BUDGET_HIGH` | `review.budgetMaxFiles: 10`, `review.budgetMaxLines: 400` |
  | `SCOPE_MATCHES_OLD_MIGRATION` | `review.migrationDirs: ["src/main/resources/db/migration"]` |
  | `CASE_VALUE_NOT_IN_BRD` | `review.valueFields: [amount, limit, balance, fee, rate, max, min]` |

  Các mã còn lại: `SCOPE_OVERLAP`, `DB_TOUCHES_EXISTING`, `DEPENDENCY_ADDED`, `BRD_RULE_NO_ID`, `CASES_RULE_UNCOVERED`, `ASSUMPTIONS_EMPTY` (INFO), `DB_CREATE_ONLY` (INFO).
- **Nguồn giả định** ∈ `tự chọn`, `suy ra từ BRD`, `câu hỏi mở đã trả lời`, `ngoài phạm vi`. Dòng không ghi nguồn hợp lệ thì có source `""`.
- **Tương thích ngược:** tính năng cũ không có mục `## Giả định`, không có `BR-n`, không có `rules:` vẫn đọc và in thẻ được.

## Review Focus

1. **Glob dạng lạ** như `**`, `*.java` ở gốc, `{a,b}`, hoặc một bước không có scope: phải ra cờ hoặc `NA`, không được exception. Test ở Task 2.
2. **SQL nháp có comment, chuỗi chứa `;`, hoặc chữ hoa/thường lẫn lộn:** không tách sai câu và không bỏ sót `ALTER`. Gặp câu không nhận dạng được thì `NA`. Test ở Task 2.
3. **Repo không phải Maven (không có `pom.xml`) hoặc `baseTree` không còn trong git:** `DEPENDENCY_ADDED` ra `NA`, không lỗi. Test ở Task 2.
4. **`brd.md` hoặc `design.md` thiếu, rỗng, hoặc mục Giả định có dòng không đúng định dạng:** thẻ vẫn in được và hiện "không đọc được …". Test ở Task 1 và Task 3.
5. **`approve` khi dựng thẻ ném lỗi:** vẫn tới bước xác nhận như cũ và in một dòng "không dựng được review card: …". Duyệt không bao giờ bị chặn vì thẻ. Test ở Task 4.

---

### Task 1: Template, prompt và đọc tài liệu (giả định, BR-n, rules của case)

**Files:**
- Modify: `aisdlc-cli/src/main/resources/templates/brd.md`. Thêm `## Giả định` ngay trước `## Câu hỏi mở`. Đổi gợi ý ở `## Quy tắc nghiệp vụ` thành dạng `- BR-1: …` (mỗi quy tắc có ID tăng dần).
- Modify: `aisdlc-cli/src/main/resources/templates/design.md`. Thêm `## Giả định` trước `## Câu hỏi mở`.
- Modify: `plugin/commands/brd.md` và `plugin/commands/design.md`. Thêm hướng dẫn liệt kê mọi lựa chọn kiểu dữ liệu, đơn vị, múi giờ, giá trị mặc định, cách xử lý lỗi hệ thống ngoài, khóa/giao dịch mà BRD hoặc ticket không ghi, mỗi dòng dạng `- <quyết định>. (<nguồn>)`.
- Modify: `plugin/commands/brd.md`: quy tắc nghiệp vụ đánh `BR-n`.
- Modify: `plugin/commands/cases.md`: mỗi case ghi `rules: [BR-n]` khi kiểm một quy tắc.
- Modify: các giới hạn độ dài hiện có trong prompt vẫn giữ nguyên.
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/Assumption.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/DocFacts.java`
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/spec/MarkdownSections.java` (thêm `public static` helper nếu cần; hoặc tạo facade công khai trong cùng gói như `OpenQuestions`).
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/spec/CaseSpec.java` (thêm `List<String> rules` ở cuối, null thành `List.of()`).
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/review/DocFactsTest.java`, cập nhật test template ở CLI nếu có.

**Interfaces:**
- Produces:
  - `record Assumption(String text, String source)`
  - `record BrdDoc(List<Rule> rules, int acceptanceCriteria, List<String> outOfScope, List<String> answeredQuestions, List<Assumption> assumptions, boolean assumptionsSection)`
  - `record Rule(String id /* "BR-1" hoặc null */, String text)`
  - `record DesignDoc(List<Assumption> assumptions, boolean assumptionsSection, List<String> apiLines /* dòng có METHOD /path */, List<String> statusCodes /* 3 chữ số trong mục API, đã sắp xếp, không trùng */)`
  - `DocFacts.brd(String markdown): BrdDoc`
  - `DocFacts.design(String markdown): DesignDoc`
  - `CaseSpec.rules(): List<String>`

- [ ] **Step 1: Viết test thất bại** (`DocFactsTest`)
  - `brdParsesRuleIdsAssumptionsAndCounts`: đầu vào có `## Quy tắc nghiệp vụ` gồm `- BR-1: Hạn mức 50.000.000 VND/ngày` và `- Không ID`; `## Tiêu chí chấp nhận` có 3 dòng `- `; `## Ngoài phạm vi` có 2 dòng; `## Giả định` có `- Số tiền BIGINT, đơn vị VND. (tự chọn)` và `- Ngày tính theo giờ VN.`; `## Câu hỏi mở` có `- [x] Có tính phí? → không` và `- [ ] Còn mở?`. Kết quả mong đợi:
    - `rules` = `[Rule("BR-1","Hạn mức 50.000.000 VND/ngày"), Rule(null,"Không ID")]`;
    - `acceptanceCriteria` = 3, `outOfScope.size()` = 2;
    - `assumptions` = `[("Số tiền BIGINT, đơn vị VND.","tự chọn"), ("Ngày tính theo giờ VN.","")]`, `assumptionsSection` = true;
    - `answeredQuestions` = `["Có tính phí? → không"]`.
  - `brdWithoutAssumptionsSection`: `assumptionsSection` = false, `assumptions` rỗng.
  - `brdIgnoresHtmlComments`: dòng `- …` nằm trong `<!-- -->` không được tính.
  - `brdEmptyOrNullInputDoesNotThrow`: `DocFacts.brd("")` và `DocFacts.brd(null)` trả về các danh sách rỗng.
  - `designApiLinesAndStatuses`: mục `## API` có `POST /api/v1/limits/check → 200, 400, 404` thì `apiLines` có đúng dòng đó và `statusCodes` = `["200","400","404"]`.
  - `caseSpecRulesOptional`: YAML case không có `rules` thì `rules()` = `[]`; có `rules: [BR-1]` thì đọc ra `["BR-1"]`.
  - Template: `brd.md` chứa `## Giả định` đứng trước `## Câu hỏi mở` và chứa `BR-1`; `design.md` chứa `## Giả định`. Đặt ở test CLI nơi đang kiểm template, nếu có; nếu không thì đọc resource trong `DocFactsTest`.
- [ ] **Step 2: Chạy** `mvn -q -pl aisdlc-core,aisdlc-cli -am test`. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.**
  - Tái dùng cách tách mục và bỏ comment của `MarkdownSections` (NFC, bỏ `<!-- -->`).
  - Nguồn của giả định là ngoặc đơn cuối dòng, chỉ khi nội dung ngoặc thuộc tập nguồn hợp lệ.
  - `BrdValidator` **không** bắt buộc có mục Giả định.
- [ ] **Step 4: Chạy** `mvn -q verify -Dskip.ui` và `bash plugin/test/plugin-files-test.sh`. Kỳ vọng: PASS, kể cả test giới hạn độ dài prompt.
- [ ] **Step 5: Commit** `feat(review): mục Giả định, ID quy tắc BR-n, rules của case, đọc tài liệu`.

### Task 2: Phân tích repo: danh sách file, scope, SQL, budget, dependency (cờ không phụ thuộc BRD)

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/ReviewFlag.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/ReviewSettings.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/ScopeFlags.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/SqlFacts.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/DependencyFlags.java`
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/git/GitRepo.java` (thêm `listFiles(): List<String>`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/model/RepoConfig.java` (thêm `ReviewSettings review()`, đọc mục `review:`, thiếu thì dùng mặc định)
- Modify: `aisdlc-cli/src/main/resources/templates/config.yaml` (thêm mục `review:` có chú thích và giá trị mặc định)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/review/{ScopeFlagsTest,SqlFactsTest,DependencyFlagsTest}.java`

**Interfaces:**
- Consumes: `ScopeMatcher.matches(List<String>, String)`, `StepSpec(id,title,scope,budget)`, `GitRepo.fileAt(treeish, path)`.
- Produces:
  - `record ReviewFlag(String code, Level level, String message, String hint, List<String> refs)`, với `enum Level { WARN, INFO, NA }`. `refs` là id bước, đường dẫn hoặc tên bảng, đã sắp xếp.
  - `record ReviewSettings(int scopeMaxFiles, int budgetMaxFiles, int budgetMaxLines, List<String> migrationDirs, List<String> valueFields)`, cùng `ReviewSettings.DEFAULTS`.
  - `GitRepo.listFiles()`: đầu ra của `git ls-files -z --cached --others --exclude-standard`, đường dẫn dùng `/`, đã sắp xếp, không trùng.
  - `ScopeFlags.matchedFiles(StepSpec step, List<String> files): List<String>`
  - `ScopeFlags.flags(List<StepSpec> steps, List<String> files, ReviewSettings s): List<ReviewFlag>`. Hàm này sinh `SCOPE_OVERLAP`, `SCOPE_TOO_WIDE`, `SCOPE_MATCHES_OLD_MIGRATION` và `BUDGET_HIGH`.
  - `record SqlStatement(String kind /* CREATE_TABLE|ALTER_TABLE|DROP|TRUNCATE|UPDATE|DELETE|INSERT|OTHER */, String table /* chữ thường, có thể null */, boolean hasWhere, List<String> columns /* chỉ CREATE_TABLE */, String text /* rút gọn 120 ký tự */)`
  - `SqlFacts.parse(String sql): List<SqlStatement>`
  - `SqlFacts.existingTables(Path repoRoot, List<String> files, ReviewSettings s): Set<String>`. Hàm này lấy tên bảng từ các câu `CREATE TABLE` trong file `.sql` nằm dưới `migrationDirs`.
  - `SqlFacts.flags(List<SqlStatement> drafts, Set<String> existing): List<ReviewFlag>`. Hàm này sinh `DB_TOUCHES_EXISTING` (WARN), `DB_CREATE_ONLY` (INFO) và `NA` khi có câu `OTHER`.
  - `DependencyFlags.flags(GitRepo git, String baseTree, Path repoRoot, List<String> files): List<ReviewFlag>`. Hàm này so tập `groupId:artifactId` trong mọi `pom.xml` ở `baseTree` với cây làm việc, và sinh `DEPENDENCY_ADDED` (refs = các tọa độ mới).

- [ ] **Step 1: Viết test thất bại**
  - `ScopeFlagsTest.pay1OverlapAndOldMigration`. Đầu vào:
    - S1 có scope `["src/main/java/**/limit/web/**"]`, budget (1,40);
    - S2 có scope `["src/main/java/**/limit/**","src/main/resources/db/migration/**"]`, budget (3,80);
    - files: `src/main/java/com/example/fixture/FixtureApplication.java`, `src/main/resources/db/migration/V1__init.sql`, `pom.xml`.

    Kết quả mong đợi đúng hai cờ WARN:
    - `SCOPE_OVERLAP` với refs `["S1","S2"]`, message nhắc `limit/web`;
    - `SCOPE_MATCHES_OLD_MIGRATION` với refs `["S2","src/main/resources/db/migration/V1__init.sql"]`.

    Không có cờ `SCOPE_TOO_WIDE` hay `BUDGET_HIGH`.
  - Phát hiện chồng lấn phải chạy được **cả khi chưa có file nào khớp**. Cách làm: sinh đường dẫn mẫu từ glob của bước này (`**/` thành `aisdlcx/`, `**` ở cuối thành `aisdlcx/AisdlcX.java`, `*` thành `aisdlcx`), rồi kiểm glob của bước kia, theo cả hai chiều.
  - `ScopeFlagsTest.fix2LiteNoFlags`: một bước `S1` có scope `["src/main/java/**/fee/**"]` → không cờ.
  - `ScopeFlagsTest.tooWide`:
    - scope `["**"]` → `SCOPE_TOO_WIDE`;
    - scope `["src/**"]` → `SCOPE_TOO_WIDE`;
    - 31 file khớp với `scopeMaxFiles=30` → `SCOPE_TOO_WIDE`.
  - `ScopeFlagsTest.oddGlobsDoNotThrow`: scope `["{a,b}/**"]`, `["*.java"]` và `[]` không ném lỗi. Scope rỗng sinh cờ `NA` với message "bước không có scope".
  - `ScopeFlagsTest.budgetHigh`: budget (11,40) → `BUDGET_HIGH`; budget (5,401) → `BUDGET_HIGH`.
  - `SqlFactsTest.splitsAndClassifies`:
    - Đầu vào: `-- c;\nCREATE TABLE daily_limit (account_id VARCHAR(20) PRIMARY KEY, limit_amount BIGINT NOT NULL);\ninsert into x values ('a;b');\nalter table ACCOUNT add column y int;\nUPDATE account SET y=1;\nDELETE FROM daily_limit WHERE account_id='a';\nGRANT SELECT ON x TO y;`
    - Kết quả: các kind `CREATE_TABLE(daily_limit, columns [account_id, limit_amount])`, `INSERT(x)`, `ALTER_TABLE(account)`, `UPDATE(account, hasWhere=false)`, `DELETE(daily_limit, hasWhere=true)`, `OTHER`.
  - `SqlFactsTest.flags` với `existing = {account}`:
    - đầu vào trên → `DB_TOUCHES_EXISTING` WARN, refs `["account"]`, áp cho cả ALTER lẫn UPDATE không có WHERE;
    - câu `OTHER` → thêm cờ `NA`;
    - chỉ có `CREATE TABLE` trỏ khóa ngoại vào bảng có sẵn → `DB_CREATE_ONLY` INFO;
    - `DELETE … WHERE` trên bảng có sẵn → không có `DB_TOUCHES_EXISTING`.
  - `SqlFactsTest.existingTablesFromMigrations`: `V1__init.sql` có `CREATE TABLE account (...)` thì kết quả là `{account}`.
  - `DependencyFlagsTest`:
    - repo git tạm, commit `pom.xml` có `junit:junit`; ghi lại tree làm base;
    - thêm `com.fasterxml.jackson.core:jackson-databind` vào working tree → `DEPENDENCY_ADDED` với refs `["com.fasterxml.jackson.core:jackson-databind"]`;
    - không có `pom.xml` → cờ `NA`;
    - baseTree không tồn tại → cờ `NA`, không ném lỗi.
  - `RepoConfigTest`, thêm hai test: thiếu mục `review:` thì dùng `DEFAULTS`; `review.scopeMaxFiles: 5` thì đọc ra 5.
- [ ] **Step 2: Chạy** `mvn -q -pl aisdlc-core test`. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.**
  - Tách câu SQL: bỏ comment `--` và `/* */`, tôn trọng chuỗi `'...'` (có `''` bên trong), rồi tách theo `;`. Không parse SQL đầy đủ.
  - Đọc `pom.xml`: lấy `<dependency><groupId>…</groupId><artifactId>…</artifactId>` bằng regex hoặc DOM của JDK, bỏ qua `<dependencyManagement>`. Lỗi khi parse thì ra `NA`.
- [ ] **Step 4: Chạy** `mvn -q verify -Dskip.ui`. Kỳ vọng: PASS.
- [ ] **Step 5: Commit** `feat(review): cờ scope/budget/DB/dependency và danh sách file từ git`.

### Task 3: ReviewCard và cờ dựa trên BRD/case

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/ReviewCard.java` (record và các record con)
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/ReviewCardBuilder.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/review/CaseFlags.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/review/{ReviewCardBuilderTest,CaseFlagsTest}.java`
- Test resources: `aisdlc-core/src/test/resources/review/pay1/` (chép `e2e/solutions/PAY-1/{brd.md,design.md,design.yaml,cases.yaml,db/*}`) và `review/fix2/`, kèm `V1__init.sql` từ `e2e/fixture-service/src/main/resources/db/migration/`.

**Interfaces:**
- Consumes: Task 1 (`DocFacts`, `BrdDoc`, `DesignDoc`, `Assumption`, `CaseSpec.rules()`) và Task 2 (`ReviewFlag`, `ReviewSettings`, `ScopeFlags`, `SqlFacts`, `DependencyFlags`, `GitRepo.listFiles()`). Ngoài ra dùng `EffectiveSteps.of(flow, design, cases)`, `FeaturePaths`, `FeatureMeta.baseTree()`, `Gate`, `Flow`.
- Produces:
  - `record ReviewCard(String gate, List<ReviewFlag> flags, List<Assumption> assumptions, boolean assumptionsSection, BrdPart brd, DesignPart design, CasesPart cases)`. Các phần không thuộc cổng đang xét là null; JSON bỏ field null.
  - `record BrdPart(int rules, int rulesWithId, int acceptanceCriteria, List<String> outOfScope, List<String> answeredQuestions)`
  - `record DesignPart(List<StepPart> steps, List<String> apiLines, List<String> statusCodes, List<ApiRow> apiRows, List<TablePart> tables, List<SqlStatementView> touchesExisting)`
  - `tables` gồm bảng mới (`NEW`) và bảng có sẵn bị tham chiếu (FK) hoặc bị sửa (`EXISTING`, `ALTERED`, `DROPPED`), kèm cột. Đây là dữ liệu cho hình ER diff của R2.
  - `record StepPart(String id, String title, List<String> scope, Budget budget, int matchedCount, List<String> matchedFiles /* tối đa 20 */, List<String> plannedFiles /* đích migration nháp db/*.sql trong migrationDirs[0] mà glob của bước khớp */, List<String> emptyGlobs /* glob chưa khớp file nào: "sẽ tạo mới" */)`
  - `record TablePart(String name, String status /* NEW|EXISTING|ALTERED|DROPPED */, List<SqlColumn> columns)`, với `SqlColumn` lấy từ Task 2.
  - `record SqlStatementView(String kind, String table, String text)`
  - `record CasesPart(List<CaseRow> rows, Map<String, Map<String, Integer>> coverage /* bước → loại → số case, TreeMap */, Map<String, Map<String, List<String>>> ruleMatrix /* BR-n → loại → id case; null khi BRD không có ID */)`
  - `record CaseRow(String id, String step, String kind, String title, String given /* ≤80 ký tự */, String expect /* "<status> <body/db rút gọn ≤80>" */, List<String> rules)`
  - `ReviewCardBuilder.build(ReviewInput in, Gate gate): ReviewCard`, với `record ReviewInput(FeaturePaths paths, FeatureMeta meta, Flow flow, RepoConfig config, GitRepo git)`. Builder tự đọc các file. Thiếu file nào thì cờ `NA` "không đọc được <file>".
  - `CaseFlags.valuesNotInBrd(CasesSpec cases, String brdMarkdown, List<String> valueFields): List<ReviewFlag>`
  - `CaseFlags.rules(BrdDoc brd, CasesSpec cases): List<ReviewFlag>` sinh `BRD_RULE_NO_ID` và `CASES_RULE_UNCOVERED`.
  - Nội dung thẻ theo cổng (spec §4):
    - **BRD:** `brd` + assumptions + cờ `BRD_RULE_NO_ID` + `ASSUMPTIONS_EMPTY` (INFO).
    - **DESIGN:** `design` + assumptions của `design.md` + cờ scope/budget/SQL, kể cả cờ INFO `DB_CREATE_ONLY` (cờ "an toàn").
    - **CASES:** `cases` + cờ case. Luồng LITE không có BRD, nên **bỏ** `CASE_VALUE_NOT_IN_BRD`, `BRD_RULE_NO_ID` và `CASES_RULE_UNCOVERED`, `ruleMatrix` null, và thêm cờ scope/budget từ `cases.yaml`.
    - **REVIEW:** chỉ `flags`, gồm `DEPENDENCY_ADDED` cộng các cờ scope/SQL của DESIGN (luồng FULL) để dòng đầu báo cáo tóm tắt được.

- [ ] **Step 1: Viết test thất bại**
  - `ReviewCardBuilderTest.pay1DesignHasExactlyTwoWarnFlags`: dựng repo git tạm chứa fixture PAY-1 và `V1__init.sql` → card DESIGN có đúng hai cờ mức HIGH hoặc WARN: `SCOPE_OVERLAP` (HIGH) và `SCOPE_MATCHES_OLD_MIGRATION` (WARN), cộng `DB_CREATE_ONLY` (INFO). Đây là yêu cầu ở spec §11.
  - `ReviewCardBuilderTest.pay1DesignErAndPlanned`:
    - `tables` có `daily_limit` (NEW, cột `account_id` có constraint `PK` và `FK`, references `account(id)`) và `account` (EXISTING);
    - `StepPart` của S2 có `plannedFiles` chứa `src/main/resources/db/migration/V2__daily_limit.sql` (hoặc tên file nháp thật của PAY-1).
  - `ReviewCardBuilderTest.pay1DesignParts`:
    - `design.steps` có id `[S1,S2]`;
    - `newTables` chứa `daily_limit`;
    - `touchesExisting` rỗng;
    - `apiLines` không rỗng.
  - `ReviewCardBuilderTest.fix2HasNoWarnFlags`: card CASES của FIX-2 (LITE) không có cờ `WARN`.
  - `ReviewCardBuilderTest.casesRowsAndCoverage`: card CASES của PAY-1 có `rows.size()` bằng số case trong `cases.yaml` và `coverage.get("S2")` có khóa `happy`.
  - `ReviewCardBuilderTest.deterministicJson`: build hai lần thì JSON (Jackson, sắp xếp khóa) giống hệt nhau.
  - `ReviewCardBuilderTest.missingBrdGivesNaNotException`: xóa `brd.md` → card BRD có cờ `NA` với message chứa `brd.md`.
  - `ReviewCardBuilderTest.legacyFeatureWithoutAssumptions`: không có mục Giả định → `assumptionsSection=false` và có cờ INFO `ASSUMPTIONS_EMPTY` với message "Agent không ghi giả định nào".
  - `CaseFlagsTest.valueNotInBrd`:
    - BRD chứa `50.000.000`; case có `then.body: {remaining: 30000000}` và `when.body: {amount: 20000000}`;
    - `valueFields` mặc định;
    - kết quả: `20000000` (khớp field `amount`) không có trong BRD → `CASE_VALUE_NOT_IN_BRD`, refs `["C3"]`;
    - `remaining` không thuộc `valueFields` nên không xét;
    - số `50000000` có trong BRD dạng `50.000.000` → không cờ (bỏ dấu `.`, `,`, `_` khi so).
    - Số ≤ 2 chữ số thì bỏ qua.
  - `CaseFlagsTest.ruleCoverage`: BRD có `BR-1`, `BR-2`; case chỉ có `rules: [BR-1]` → `CASES_RULE_UNCOVERED` refs `["BR-2"]`. BRD có một quy tắc không ID → `BRD_RULE_NO_ID`. BRD không có ID nào → `ruleMatrix` null.
- [ ] **Step 2: Chạy** `mvn -q -pl aisdlc-core test`. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.** Builder không ném lỗi ra ngoài với dữ liệu hỏng: mỗi phần bọc trong try/catch và chuyển lỗi thành cờ `NA`.
- [ ] **Step 4: Chạy** `mvn -q verify -Dskip.ui`. Kỳ vọng: PASS.
- [ ] **Step 5: Commit** `feat(review): ReviewCard theo cổng, cờ giá trị case và độ phủ quy tắc`.

### Task 4: CLI — `aisdlc review`, `approve` in thẻ, snapshot mang `reviewCards`

**Files:**
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/ReviewCommand.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/ReviewCardText.java`
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/Main.java` (đăng ký lệnh)
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/ApproveCommand.java`. In thẻ sau `summary(...)` và trước `ctx.confirmDev(...)`. Bọc trong try/catch: lỗi thì in một dòng `Không dựng được review card: <lỗi>` rồi đi tiếp.
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/web/SnapshotBuilder.java` và `FeatureSnapshot.java`. Thêm `Map<String, ReviewCard> reviewCards` (tùy chọn) và dựng cho mọi cổng trong luồng. Lỗi ở cổng nào thì bỏ cổng đó.
- Modify: `plugin/commands/*.md`, ở những chỗ hướng dẫn dev duyệt: thêm câu "Trước khi duyệt, `aisdlc review <gate>` in review card".
- Modify: `docs/README.vi.md` (mục ngắn "Review card và cờ đỏ").
- Modify: `e2e/run-e2e.sh`. Sau `submit design` của PAY-1, chạy `aisdlc review design`, kiểm có `SCOPE_OVERLAP` và `SCOPE_MATCHES_OLD_MIGRATION` và lệnh chạy dưới 1 giây. FIX-2 `aisdlc review cases` không có dòng WARN.
- Test: `aisdlc-cli/src/test/java/com/aisdlc/cli/ReviewCommandTest.java`, `ReviewCardTextTest.java`; cập nhật `DevOnlyCommandTest` và `CliFlowTest`.

**Interfaces:**
- Consumes: Task 3 `ReviewCardBuilder.build(ReviewInput, Gate)`, `ReviewCard`.
- Produces:
  - `ReviewCardText.render(ReviewCard card, int width, boolean color): String`
  - `aisdlc review <gate> [--json]`: exit 0, in thẻ dạng chữ hoặc JSON. Lệnh không cần console, không đổi trạng thái và không gửi web.
- Định dạng chữ (bố cục cố định, thứ tự theo spec §2.1):
  - Tiêu đề `Review card — <KEY> · cổng <GATE>`.
  - `Cờ đỏ (N)` liệt kê HIGH trước, rồi WARN, NA, INFO. Mỗi cờ có dòng `  [WARN] CODE: message` và dòng `         → hint`.
  - `Giả định (N)` dạng `  - text (nguồn)`, hoặc dòng "Agent không ghi giả định nào".
  - Phần riêng theo cổng (bảng bước, DB, API, bảng case một dòng mỗi case, độ phủ).
  - Mọi dòng ≤ width; dòng dài bị cắt và thêm `…`.

- [ ] **Step 1: Viết test thất bại**
  - `ReviewCardTextTest.linesWithinWidthAndOrder`: card có 1 WARN, 1 INFO và 12 giả định. Kỳ vọng:
    - mọi dòng ≤ 100 cột;
    - WARN đứng trước INFO;
    - chỉ hiện 10 giả định kèm dòng `… và 2 nữa`;
    - không có ký tự `\u001b` khi `color=false`.
  - `ReviewCommandTest.pay1DesignPrintsTwoFlags`: dựng repo theo cách `TestRepo` hiện có, chép fixture PAY-1. Kỳ vọng:
    - `aisdlc review design` exit 0;
    - stdout chứa `SCOPE_OVERLAP` và `SCOPE_MATCHES_OLD_MIGRATION`;
    - thời gian dưới 1000 ms (đo quanh `Main.run`).
  - `ReviewCommandTest.jsonMatchesBuilder`: `--json` trả JSON parse được, `gate` là `"DESIGN"`.
  - `ReviewCommandTest.runsWithoutConsole`: console giả trả rỗng vẫn exit 0.
  - `DevOnlyCommandTest.approvePrintsCardBeforeConfirm`: trong stdout của `approve design`, chỉ số dòng `Review card —` nhỏ hơn chỉ số lời nhắc `Gõ lại feature key`.
  - `DevOnlyCommandTest.approveStillWorksWhenCardFails`: làm hỏng `design.yaml` sau khi nộp. Cổng lúc đó là STALE hoặc không cho duyệt như cũ, nhưng không có stack trace và có dòng `Không dựng được review card` (hoặc card có cờ NA).
  - `HookCommandTest` / `BashHeuristicsTest`: Bash `aisdlc review design` **không** bị chặn.
  - `SnapshotBuilderTest.includesReviewCards`: snapshot PAY-1 có `reviewCards.DESIGN.flags` chứa `SCOPE_OVERLAP` và `schemaVersion` vẫn là 1.
- [ ] **Step 2: Chạy** `mvn -q -pl aisdlc-cli -am test`. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy**:
  - `mvn -q verify`
  - `bash plugin/test/plugin-files-test.sh`
  - `WEB=0 bash e2e/run-e2e.sh`
  - `bash e2e/run-e2e.sh`

  Kỳ vọng: PASS và `E2E PASS`.
- [ ] **Step 5: Commit** `feat(cli): aisdlc review, approve in review card, snapshot mang reviewCards`.

## Thứ tự và song song

- Task 1 và Task 2 sửa các file khác nhau, chỉ cùng gói mới `review`, nên **chạy song song** được trên hai worktree. Phần chung duy nhất là `RepoConfig` (Task 2) và `CaseSpec` (Task 1), không trùng file.
- Task 3 cần cả Task 1 và Task 2. Task 4 cần Task 3.
