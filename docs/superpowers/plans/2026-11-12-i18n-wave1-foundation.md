# Song ngữ EN/VI — Đợt 1 (nền) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Đặt nền cho song ngữ: gói `internal/i18n` với ba test catalog, khóa `locale` trong `aisdlc/config.yaml`,
`setup --locale`, template `vi`/`en`, bảng tiêu đề mục song ngữ cho validator và `aisdlc status` in `Locale:`.

**Architecture:** Kiểu gettext: chuỗi tiếng Việt là khóa, `internal/i18n/en.go` là catalog tiếng Anh. Locale mặc định
của tiến trình đặt một lần ở `cli.runWith` (từ config của repo hoặc `AISDLC_LOCALE`) và ở `aisdlc-harness` (từ
`AISDLC_LOCALE` do `aisdlc` truyền xuống). Tiêu đề mục markdown tra theo ID qua một bảng song ngữ trong `internal/spec`.

**Tech Stack:** Go 1.26 (`go/ast`, `go/parser`, `sync/atomic`, `embed`), cobra. Không thêm dependency.

**Spec:** `docs/superpowers/specs/2026-11-11-i18n-en-vi-design.md` (đợt 1 trong mục "Chia đợt giao").

**Nhánh:** `feat/i18n-foundation`, tách từ `feat/llm-risk` (PR #13 chưa merge). Sau khi #13 merge thì rebase lên `main`.

## Global Constraints

- Locale chỉ nhận `vi` và `en` (so sau `TrimSpace` + chữ thường). Thiếu khóa → `vi`. Giá trị khác trong
  `aisdlc/config.yaml` → exit 2, thông báo nêu giá trị sai. `AISDLC_LOCALE` sai giá trị → bỏ qua, dùng `vi`
  (*ruling: biến môi trường sai không được làm hỏng hook*).
- Repo không có `locale` phải ra byte-for-byte như hiện tại: template `vi` giữ nguyên byte, thông báo giữ nguyên, hash
  artifact không đổi. Test và E2E hiện có không đổi kỳ vọng, **trừ** kỳ vọng của `aisdlc status` (dòng `Locale:` mới).
- Ngoại lệ có chủ ý duy nhất cho template `vi`: `config.yaml` có thêm dòng `locale: vi` (spec: "config.yaml bản nào
  cũng có dòng `locale:`"). `config.yaml` không nằm trong hash artifact nào.
- Mã lỗi/mã cờ (`code`), tên cổng, khóa JSON/YAML không dịch.
- Web server (`internal/web/...`) không gọi `i18n.T`/`i18n.Tf` dạng gói; đợt này không đụng web.
- Không thêm dependency Go hay npm.
- Commit kết thúc bằng `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; không `--no-verify`; pre-commit
  (Semgrep, Trivy) phải qua.
- Văn phong tiếng Anh: ngắn, rõ, cùng thuật ngữ `docs/user-guide.md` (gate, step, scope, budget, case suite, eval).

## Review Focus

1. Repo cũ không có `locale`: `aisdlc init` phải chép template giống hệt byte so với trước đợt này → test băm sha256
   từng template `vi` (Task 4).
2. `locale: EN` / `locale: " en "` được nhận là `en`; `locale: fr` → exit 2 có thông báo, kể cả khi chạy hook
   (Task 2).
3. BRD trộn tiêu đề hai ngôn ngữ (`## Goal` cùng `## Trong phạm vi`) vẫn hợp lệ vì tra theo ID; tiêu đề khác hoa thường
   (`## goal`) cũng nhận (Task 3).
4. Chạy `aisdlc` ngoài repo với `AISDLC_LOCALE=en` → locale `en`; trong repo có `locale: vi` thì config thắng biến môi
   trường (Task 2).
5. Test chạy song song trong tiến trình (`t.Parallel` ở `internal/cli`) không gây data race khi đặt locale mặc định
   → `SetDefault`/`Default` dùng `atomic`, `go test -race ./internal/i18n ./internal/cli` sạch (Task 1, Task 2).

---

### Task 1: Gói `internal/i18n` và ba test catalog

**Files:**
- Create: `internal/i18n/i18n.go`, `internal/i18n/en.go`, `internal/i18n/i18n_test.go`,
  `internal/i18n/catalog_test.go`, `internal/i18n/testdata/bare-vietnamese-allow.txt`

**Interfaces:**
- Produces:
  - `type Locale string`; `const VI Locale = "vi"`, `EN Locale = "en"`.
  - `func Parse(s string) (Locale, error)`: `TrimSpace` + `ToLower`; `""` → `VI, nil`; khác `vi`/`en` → error
    `Tf("Giá trị locale không hợp lệ: %q (chỉ nhận vi hoặc en)", s)`.
  - `func FromEnv(getenv func(string) (string, bool)) Locale`: đọc `AISDLC_LOCALE`; thiếu hoặc sai → `VI`.
  - `func SetDefault(l Locale)`, `func Default() Locale` (lưu bằng `atomic.Value`; chưa đặt → `VI`).
  - `func T(key string) string`, `func Tf(format string, args ...any) string` (dùng `Default()`).
  - `func (l Locale) T(key string) string`, `func (l Locale) Tf(format string, args ...any) string`.
  - `en.go`: `var en = map[string]string{...}` (đợt này chỉ có các mục mà Task 2–3 thêm).
  - Dịch: `l == EN` và có mục → bản tiếng Anh; ngược lại trả nguyên khóa. `Tf` = `fmt.Sprintf(l.T(format), args...)`.

- [ ] **Step 1: Viết test hành vi** trong `i18n_test.go`:
  - `TestParse`: `""`→VI, `"vi"`→VI, `" EN "`→EN, `"fr"`→error chứa `"fr"`.
  - `TestTranslate`: với một mục tạm thêm vào `en` trong test (`en["Xin chào %s"]="Hello %s"`, xóa bằng `t.Cleanup`):
    `EN.Tf("Xin chào %s","An")=="Hello An"`, `VI.Tf(...)=="Xin chào An"`, `EN.T("Không có trong catalog")` trả nguyên
    khóa.
  - `TestDefaultConcurrent`: 50 goroutine gọi xen kẽ `SetDefault(EN/VI)` và `Default()`; chạy với `-race` phải sạch.
  - `TestFromEnv`: `AISDLC_LOCALE=en`→EN, `=xx`→VI, thiếu→VI.

- [ ] **Step 2: Viết ba test catalog** trong `catalog_test.go`. Quét AST (`go/parser`, `parser.ParseFile` không cần
  type-check) mọi file `.go` không phải `_test.go` dưới gốc module (tìm `go.mod` đi ngược từ `os.Getwd()`), bỏ qua
  thư mục `.git`, `node_modules`, `.superpowers`, `e2e`.
  - Lời gọi dịch = `CallExpr` có `Fun` là `SelectorExpr` với `Sel.Name` ∈ {`T`,`Tf`}, và đối số đầu là literal chuỗi
    (hoặc phép `+` của các literal, gộp lại). Đối số không phải literal thì bỏ qua (dùng cho chuỗi đã lưu ở đợt 4).
  - `TestCatalogComplete`: mỗi khóa literal có mục trong `en`; dãy động từ `%` (regex
    `%[-+# 0]*(\[\d+\])?\d*(\.\d+)?[a-zA-Z%]`) của khóa và của bản dịch giống hệt, đúng thứ tự. Thông báo lỗi nêu
    `file:line` và khóa.
  - `TestCatalogNoOrphans`: mọi khóa của `en` được ít nhất một lời gọi literal dùng.
  - `TestNoBareVietnamese`: mọi `BasicLit` chuỗi có ký tự tiếng Việt (regex các chữ có dấu tiếng Việt và `đĐ`, hoa
    lẫn thường) phải nằm trong đối số của một lời gọi dịch; bỏ qua struct tag (`Field.Tag`), `internal/i18n/en.go`,
    và các file có trong allowlist. Allowlist: mỗi dòng `<đường dẫn từ gốc module>  # <lý do>`, dòng trống và dòng
    bắt đầu `#` bỏ qua. Test **cũng** báo lỗi khi một mục allowlist trỏ tới file không tồn tại hoặc file không còn
    literal tiếng Việt trần nào (để danh sách chỉ thu hẹp).
  - Cờ `go test ./internal/i18n -run TestNoBareVietnamese -update-allowlist` ghi lại allowlist: giữ nguyên mục có lý do
    khác `chưa chuyển (đợt N)`, thêm mọi file vi phạm còn lại với lý do `chưa chuyển (đợt N)` theo bảng gói ở mục
    "Chia đợt giao" của spec (cli/policy/gate/step/spec/model/snapshot/gitx/yamlx/fsx/cmd → 2; harness/eval/review/
    risk/citest/lang/run/webclient → 3; web → 4; còn lại → 5), sắp theo đường dẫn.

- [ ] **Step 3: Chạy test, thấy fail** (`go test ./internal/i18n/...` → không biên dịch được vì chưa có `i18n.go`).

- [ ] **Step 4: Viết `i18n.go` và `en.go`** theo Interfaces. `en.go` có comment nói quy tắc: khóa là chuỗi tiếng Việt
  nguyên văn trong code, động từ `%` giống hệt.

- [ ] **Step 5: Sinh allowlist** bằng `-update-allowlist`, đọc lại file: mọi dòng phải có lý do. Rồi chạy:
  `go test -race ./internal/i18n/...` → PASS.

- [ ] **Step 6: Commit**
  `git add internal/i18n && git commit -m "feat(i18n): gói dịch kiểu gettext và ba test catalog"`

### Task 2: Khóa `locale`, locale mặc định của tiến trình, `status` in `Locale:`

**Files:**
- Modify: `internal/model/config.go` (trường `Locale`), `internal/cli/cli.go` hoặc file chứa `runWith` (đặt locale),
  `internal/cli/harnessproc.go` (truyền `AISDLC_LOCALE`), `internal/harness/harnesscmd` (`Main` đặt locale),
  `internal/cli/cmd_status.go`, test tương ứng, `internal/i18n/en.go`
- Test: `internal/model/config_test.go`, `internal/cli/locale_test.go` (mới), test của `status`

**Interfaces:**
- Consumes: `i18n.Parse`, `i18n.FromEnv`, `i18n.SetDefault`, `i18n.Default` (Task 1).
- Produces:
  - `model.RepoConfig.Locale i18n.Locale`; `configFrom` gọi `i18n.Parse(str(n,"locale",""))`, lỗi đi qua `rd` như
    các giá trị sai khác.
  - `func repoLocale(ctx *Context) (i18n.Locale, error)` trong `internal/cli/locale.go`: trong repo (`FindRepo`) →
    đọc **chỉ** khóa `locale` của `aisdlc/config.yaml` (YAML hỏng ở khóa khác không làm lỗi bước này); ngoài repo hoặc
    config không có khóa → `i18n.FromEnv(ctx.Env)`. *Ruling:* trong repo mà config thiếu khóa thì vẫn xét
    `AISDLC_LOCALE` trước khi rơi về `vi` — spec chỉ nói "thiếu → vi" cho repo; biến môi trường là thứ dev tự đặt,
    nên tôn trọng nó; ghi vào ledger.
  - `runWith` gọi `repoLocale` trước khi dựng cây lệnh; lỗi → in `Lỗi: <msg>` ra stderr, exit 2 (như lỗi cấu hình
    khác); thành công → `i18n.SetDefault`.
  - `execHarness` đặt `cmd.Env = append(os.Environ(), "AISDLC_LOCALE="+string(i18n.Default()))`.
  - `harnesscmd.Main` gọi `i18n.SetDefault(i18n.FromEnv(...os.LookupEnv))` đầu tiên.
  - `status` dạng chữ: dòng đầu stdout luôn là `Locale: vi` hoặc `Locale: en`, kể cả khi `status` báo không có tính
    năng. `status --json` không đổi.

- [ ] **Step 1: Test config** (`config_test.go`): thiếu `locale` → `VI`; `locale: EN` → `EN`; `locale: fr` → error
  chứa `fr`.
- [ ] **Step 2: Test CLI** (`locale_test.go`, chạy qua `Run` trong tiến trình):
  - repo có `locale: en` → sau lệnh `status`, dòng đầu `Locale: en` và `i18n.Default()==EN`;
  - repo không có khóa, env `AISDLC_LOCALE=en` (qua env của Context) → `Locale: en`;
  - repo `locale: vi`, env `en` → `Locale: vi`;
  - repo `locale: fr` → exit 2, stderr chứa `fr`; cũng đúng với `aisdlc hook pre-tool-use` (lệnh hook bất kỳ đang có).
  - Mỗi test reset bằng `t.Cleanup(func(){ i18n.SetDefault(i18n.VI) })`.
- [ ] **Step 3: Chạy, thấy fail.**
- [ ] **Step 4: Cài đặt** theo Interfaces. Thông báo mới tiếng Việt viết qua `i18n.T`/`Tf` và thêm bản tiếng Anh vào
  `en.go`. Sửa kỳ vọng các test `status` dạng chữ hiện có để có dòng `Locale: vi` đầu tiên (chỉ thêm dòng, không đổi
  dòng khác). Tìm kỳ vọng `status` ở `cmd/aisdlc/main_test.go`, `internal/cli/*_test.go`, `e2e/` và
  `plugin/test/` (`grep -rn "Tính năng .* (full)\|aisdlc status" `).
- [ ] **Step 5: Chạy** `go test -race ./internal/i18n/... ./internal/model/... ./internal/cli/... ./internal/harness/...`
  và `AISDLC_SKIP_DOCKER=1 go test ./...` → PASS. Nếu allowlist có mục file vừa hết tiếng Việt trần, chạy lại
  `-update-allowlist`.
- [ ] **Step 6: Commit** `feat(cli): khóa locale trong config, AISDLC_LOCALE, status in Locale`.

### Task 3: Bảng tiêu đề mục song ngữ và validator

**Files:**
- Create: `internal/spec/sections.go`, `internal/spec/sections_test.go`
- Modify: `internal/spec/markdown.go`, `internal/spec/brd.go`, `internal/spec/designvalidator.go`,
  `internal/review/docfacts.go` (+ test), `internal/i18n/en.go`,
  `internal/i18n/testdata/bare-vietnamese-allow.txt` (thêm `internal/spec/sections.go  # bảng tiêu đề mục song ngữ`)

**Interfaces:**
- Consumes: `i18n.Locale`, `i18n.Default`, `i18n.Tf`.
- Produces (trong `internal/spec`):
  - `type SectionID string` với các hằng `SecGoal, SecInScope, SecOutOfScope, SecRules, SecAcceptance,
    SecAssumptions, SecOpenQuestions` (giá trị: `goal`, `inScope`, `outOfScope`, `rules`, `acceptance`,
    `assumptions`, `openQuestions`) — tiêu đề đúng như bảng trong spec mục "Tiêu đề mục markdown".
  - `func Heading(id SectionID, l i18n.Locale) string`.
  - `func SectionsByID(md string) map[SectionID]string`: dùng `MarkdownSections`, khớp tên mục với tiêu đề `vi`
    hoặc `en` của mỗi ID (so sau NFC, `strings.EqualFold`); cùng ID xuất hiện dưới cả hai tiêu đề thì nối nội dung
    theo thứ tự xuất hiện. `Sequence`, `API`, `DB` vẫn tra bằng tên qua `MarkdownSections` như cũ.
  - `var AssumptionSources = []struct{ ID string; VI, EN string }` với ID `own`, `inferred`, `answered`,
    `outOfScope` và chuỗi đúng như spec (`tự chọn | suy ra từ BRD | câu hỏi mở đã trả lời | ngoài phạm vi`;
    `own choice | inferred from BRD | answered open question | out of scope`).
  - `func SourceID(s string) string`: khớp `EqualFold` với VI hoặc EN, trả ID; không khớp → `""`.
  - `BoundaryTags = []string{"[biên]", "[boundary]"}`.
- `ValidateBRD(content string)` giữ chữ ký; mục bắt buộc theo ID (`goal, inScope, outOfScope, rules, acceptance`);
  thông báo thiếu/trống dùng `Heading(id, i18n.Default())` và đi qua `i18n.Tf` (khóa = câu tiếng Việt hiện có, ví dụ
  `"BRD thiếu mục bắt buộc: \"%s\"."`, `"Mục \"%s\" của BRD đang trống."`, `"Câu hỏi mở chưa được trả lời: %s"`).
- `OpenQuestions` đọc mục `SecOpenQuestions` qua `SectionsByID`.
- `review.ParseBRD`/`ParseDesign`: tra `rules`, `openQuestions`, `acceptance`, `outOfScope`, `assumptions` theo ID;
  `boundaryRe` nhận cả `[biên]` và `[boundary]`; `review.Sources` thay bằng `spec.AssumptionSources`;
  `Assumption.Source` lưu chuỗi chuẩn **của ngôn ngữ đã khớp** (VI hoặc EN) — JSON thẻ `vi` không đổi. Mọi chỗ so
  `Source` với một chuỗi cụ thể (tìm `grep -rn "\.Source\b" internal`) chuyển sang so `spec.SourceID(...)`.

- [ ] **Step 1: Test** `sections_test.go`:
  - `TestSectionsByIDBothLanguages`: BRD tiếng Anh đủ 5 mục → `ValidateBRD` không có phát hiện; BRD trộn `## Goal` +
    `## Trong phạm vi` + ... → không có phát hiện; `## goal` (thường) được nhận.
  - `TestValidateBRDMessageFollowsLocale`: thiếu mục `goal`, `i18n.SetDefault(EN)` → message chứa `"Goal"` và không
    chứa ký tự tiếng Việt; với `VI` → message đúng nguyên văn hiện có `BRD thiếu mục bắt buộc: "Mục tiêu".`
  - `TestOpenQuestionsEnglish`: `## Open questions` với `- [ ] Why?` → `["Why?"]`.
  - `TestSourceID`: `"Own choice"`→`own`, `"suy ra từ BRD"`→`inferred`, `"khác"`→`""`.
  - Trong `internal/review`: `ParseBRD` trên fixture tiếng Anh (`## Business rules` có `- BR-1: ... [boundary]`,
    `## Assumptions` có `- Amounts in VND. (own choice)`) → rule có `Boundary=true`, assumption Source `own choice`.
- [ ] **Step 2: Chạy, thấy fail.**
- [ ] **Step 3: Cài đặt** theo Interfaces; thêm bản tiếng Anh cho mọi khóa mới vào `en.go`.
- [ ] **Step 4: Chạy** `go test ./internal/spec/... ./internal/review/... ./internal/i18n/...` và
  `AISDLC_SKIP_DOCKER=1 go test ./...` → PASS; mọi test hiện có của `spec`/`review` không phải sửa kỳ vọng.
- [ ] **Step 5: Commit** `feat(spec): tiêu đề mục song ngữ tra theo ID; validator nhận cả hai ngôn ngữ`.

### Task 4: Template `vi`/`en`, `setup --locale`, `init` theo locale

**Files:**
- Move: `templates/*.md|*.yaml|*.yml` → `templates/vi/` (`git mv`, nội dung giữ nguyên byte, trừ `config.yaml` thêm
  dòng `locale: vi`)
- Create: `templates/en/` đủ 8 file: `brd.md`, `design.md`, `design.yaml`, `cases.full.yaml`, `cases.lite.yaml`,
  `config.yaml`, `CLAUDE.aisdlc.md`, `gitlab-ci.aisdlc.yml`
- Modify: `templates/templates.go`, `templates/templates_test.go`, `internal/cli/cmd_setup.go`,
  `internal/cli/cmd_init.go`, test `setup`/`init`, `CHANGELOG.md` (mục Unreleased → Added)

**Interfaces:**
- Consumes: `i18n.Locale`, `i18n.Default`, `i18n.Parse`, `i18n.FromEnv`; `spec.Heading` (tiêu đề tiếng Anh của
  template `en/brd.md`, `en/design.md` phải đúng bảng ID).
- Produces:
  - `templates.FS` nhúng `vi/* en/*`; `func Read(l i18n.Locale, name string) (string, error)`.
  - `cli.template(name)` đổi thành `template(l i18n.Locale, name string)`; `init` dùng `i18n.Default()`.
  - `setup --locale vi|en`: thứ tự `--locale` → `AISDLC_LOCALE` → `vi`; giá trị cờ sai → exit 2 (thông báo qua
    `i18n.Tf`). Template `config.yaml`, `CLAUDE.aisdlc.md`, `gitlab-ci.aisdlc.yml` lấy theo locale đã chọn. Sau khi
    chọn, `setup` gọi `i18n.SetDefault` với locale đó (in thông báo setup bằng locale mới khi đợt 2 dịch xong).
- Template `en`: dịch comment và giá trị mẫu; khóa YAML, scope, đường dẫn, lệnh, khối mermaid giữ nguyên cấu trúc.
  `en/brd.md` có đủ 7 mục theo tiêu đề `en` của bảng ID; `en/design.md` có `Sequence`, `API`, `DB`, `Assumptions`,
  `Open questions`. Nguồn giả định và thẻ biên dùng bản tiếng Anh (`own choice | …`, `[boundary]`).
  `CLAUDE.aisdlc.md` bản `en` thêm câu: agent viết tài liệu và trả lời dev bằng tiếng Anh; bản `vi` **không đổi**.

- [ ] **Step 1: Trước khi chuyển file, ghi sha256** của 7 template không phải `config.yaml` (`sha256sum templates/*.md
  templates/*.y*ml | grep -v config.yaml`) và đưa vào test `TestViTemplatesUnchanged` (map tên → sha256 hex).
- [ ] **Step 2: Test** (`templates_test.go` và test CLI):
  - `TestViTemplatesUnchanged`: `Read(VI, name)` có sha256 đúng như đã ghi.
  - `TestTemplateSetsMatch`: tập tên file trong `vi/` và `en/` bằng nhau.
  - `TestEnTemplatesHaveNoVietnamese`: không file `en/*` nào có ký tự tiếng Việt.
  - `TestEnTemplatesPassValidators`: `en/brd.md` đã điền mỗi mục một dòng nội dung → `spec.ValidateBRD` không có
    phát hiện thiếu mục; `en/design.yaml` và `en/cases.*.yaml` parse được bằng cùng hàm đọc mà `init`/`submit` dùng.
  - `TestConfigTemplatesHaveLocale`: `vi/config.yaml` có dòng `locale: vi`, `en/config.yaml` có `locale: en`; mỗi bản
    qua `model.ParseRepoConfig` ra đúng `Locale`.
  - Các test hiện có trong file (`Java`, scope Go, CI dind, CLAUDE Go flow, no impact) chạy trên **cả hai** bộ, với
    chuỗi tìm kiếm tiếng Việt của `TestClaudeTemplateDescribesGoFlow` có bản tương ứng cho `en`.
  - CLI: `setup --locale en` trong repo trống → `aisdlc/config.yaml` có `locale: en` và CLAUDE.md có mục AISDLC bản
    tiếng Anh; `setup --locale fr` → exit 2; `AISDLC_LOCALE=en setup` → như `--locale en`; `setup` không cờ, không
    env → bản `vi`.
  - CLI: repo `locale: en` → `init` (luồng full) tạo `brd.md` bằng đúng `Read(EN,"brd.md")`; repo không khóa →
    bằng `Read(VI,"brd.md")`.
- [ ] **Step 3: Chạy, thấy fail.**
- [ ] **Step 4: Cài đặt.** `git mv` các template, viết bản `en`, sửa `templates.go`, `setup`, `init`. Không chỉnh nội
  dung bản `vi` ngoài dòng `locale: vi` trong `config.yaml`. `detectService` vẫn thay đúng dòng `main:`/`dir:` của bản
  `en` (giữ đúng hai dòng mẫu đó trong `en/config.yaml`).
- [ ] **Step 5: CHANGELOG** (Unreleased → Added): khóa `locale` (`vi`/`en`), `aisdlc setup --locale`,
  `AISDLC_LOCALE`, template tiếng Anh, validator nhận tiêu đề tiếng Anh, `aisdlc status` in `Locale:`. Ghi rõ thông
  báo CLI, web, plugin vẫn tiếng Việt cho tới các đợt sau.
- [ ] **Step 6: Chạy toàn bộ**: `AISDLC_SKIP_DOCKER=1 go test -race ./...`, `go vet ./...`, `make e2e` (E2E PASS, không
  sửa kỳ vọng ngoài dòng `Locale:` của status), `bash docs/docs-check.sh`, `bash plugin/test/plugin-files-test.sh`.
- [ ] **Step 7: Commit** `feat(templates): bộ template vi/en; setup --locale; init theo locale của repo`.
