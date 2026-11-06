# AISDLC Go — Giai đoạn 5: Hoàn thiện

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bản Go dùng được trọn vẹn cho một dịch vụ Go:
- template và plugin viết cho Go;
- tài liệu tiếng Việt;
- CI cho chính repo AISDLC;
- kịch bản e2e (bản Go của `e2e/run-e2e.sh` cùng các script Playwright) pass trọn trên dịch vụ mẫu Go, tức tiêu chí nghiệm thu của spec §2.5.

**Architecture:** Không thêm tính năng mới, chỉ thay nội dung Java/Maven bằng Go/goose. E2E là shell + `jq` + Playwright như bản Java, chạy binary Go, dịch vụ mẫu `e2e/fixture-service` và web server Go (Docker Postgres). Lời giải PAY-1, FIX-2, FEE-3 viết lại bằng Go.

**Tech Stack:** bash, jq, Node + Playwright (đã có trong `e2e/` Java), Docker. GitLab CI có Docker-in-Docker.

**Spec:** `~/Documents/git/aisdlc/docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md`:
- §7: template, plugin commands, skill `cases`/`step`;
- §9: nghiệm thu e2e;
- §10 bước 5;
- §2: phản mục tiêu, đặc biệt các bước âm tính.

**Nguồn Java:** `~/Documents/git/aisdlc`:
- `e2e/` (run-e2e.sh 1125 dòng, web-*.mjs, screenshots.mjs, web-check.sh, solutions/);
- `docs/README.vi.md`, `docs/huong-dan-su-dung.md`;
- `aisdlc-cli/src/main/resources/templates`, `plugin/`.

**Phụ thuộc:**
- Task 1, 2, 5, 6 không cần 4b, có thể chạy song song với 4b.
- Task 3 cần 4a.
- Task 4 cần 4a và 4b đã merge.
- Task 7 chạy sau cùng.

## Global Constraints

- Tài liệu và thông báo bằng tiếng Việt. Giữ văn phong tài liệu Java hiện có.
- Không còn nhắc `mvn`, Maven, JUnit, JaCoCo, Flyway, `src/main/java`, `pom.xml` hay `AisdlcCasesTest`, trừ trong `docs/legacy-java/` và trong ghi chú adapter Java tương lai. Kiểm bằng `grep -rniE --exclude-dir=node_modules --exclude-dir=out 'mvn|maven|junit|jacoco|flyway|src/main/java|pom\.xml' templates plugin docs/*.md README.md e2e` (bỏ `legacy-java`): không có kết quả.
- Migration theo goose: tên `NNNNN_ten.sql`, có `-- +goose Up`. Bản nháp tính năng nằm trong `aisdlc/features/<KEY>/db/`.
- So DB trong `then.db` theo spec §6.7:
  - số so theo giá trị;
  - bool là `true`/`false`;
  - NULL khớp `null`;
  - timestamp, date dạng RFC 3339 UTC;
  - json/jsonb so theo cấu trúc;
  - còn lại so dạng text.

  Quy tắc này phải nằm trong skill `cases` và `step`.
- Plugin: binary thiếu thì fail closed như hiện tại, nhưng chỉ chặn công cụ ghi. `Read`, `Grep`, `Glob` không bị chặn (carry-over giai đoạn 2b).
- Hook p95 dưới 50 ms (spec §7). Đo lại trong e2e ở bước độ trễ hook.
- Commit có trailer:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **Dev làm theo `huong-dan-su-dung.md` trên máy sạch, chỉ có Go và Docker.** Cài plugin, `aisdlc setup`, làm hết một tính năng LITE. Không bước nào thiếu lệnh hay dùng lệnh không tồn tại. Test: Task 6 `docs-check.sh` chạy mọi khối lệnh trong tài liệu được đánh dấu `# chạy-được` trên repo tạm.
2. **CI của dịch vụ đích không có Docker socket.** `aisdlc eval --ci` báo lỗi rõ ("cần Docker cho harness: ...") rồi thoát khác 0, không treo. Test: Task 1 kiểm template CI có service `docker:dind` và `DOCKER_HOST`. Task 4 có một bước e2e chạy eval với `DOCKER_HOST` sai và kiểm thông báo cùng mã thoát.
3. **Binary plugin thiếu hoặc sai nền tảng.** Ở repo không phải AISDLC thì hook im lặng. Ở repo AISDLC thì chặn ghi nhưng cho đọc. Test: Task 2, `plugin/test/plugin-files-test.sh` có ca mới.
4. **E2E chạy lại trên cùng máy.** Có container, port hay `HOME` sót từ lần trước thì không fail oan. Test: Task 4, chạy `make e2e` hai lần liền trong CI job e2e.
5. **Dịch vụ mẫu không tắt êm.** Nhánh âm tính trong e2e: eval báo coverage N/A kèm lý do. Test: Task 4, bước "fixture bỏ qua SIGTERM", dùng biến env của fixture.

---

### Task 1: Template cho Go, `setup`

**Files:** `templates/*` (design.yaml, design.md, cases.lite.yaml, cases.full.yaml, config.yaml, CLAUDE.aisdlc.md, gitlab-ci.aisdlc.yml, brd.md); `internal/cli/cmd_setup.go` nếu output còn nhắc Java; test trong `internal/cli/setup_test.go` và `templates/templates_test.go`.

- [ ] **Step 1: Viết test.**
  - `TestTemplatesHaveNoJavaMentions`: chạy regex ở Global Constraints trên mọi template.
  - `TestDesignTemplateScopesAreGo`: scope ví dụ có dạng `internal/limit/**` và `db/migrations/**`.
  - `TestCITemplateUsesGoAndDind`: template CI có image Go, service `docker:dind`, `DOCKER_HOST`, `aisdlc eval --ci`, và không có `mvn`.
  - `TestClaudeTemplateDescribesGoFlow`: nhắc `go test`, goose và quy tắc so DB.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3: Viết lại template.**
  - `CLAUDE.aisdlc.md` cho Go: layout `cmd/`, `internal/`, migration goose, quy tắc DB, không sửa `cases.yaml` sau khi duyệt...
  - `gitlab-ci.aisdlc.yml`: `golang:1.26` + `docker:dind`, cài binary aisdlc, rồi `aisdlc eval --ci`.
  - `config.yaml`: theo §3.1, trong đó `checks` chỉ ghi chú gợi ý `go vet`, `staticcheck`, `govulncheck`, không bật sẵn.
- [ ] **Step 4:** Chạy `go test ./templates/... ./internal/cli/... -run 'Template|Setup'` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(templates): template cho dịch vụ Go`.

### Task 2: Plugin — commands, agent reviewer, skill, launcher

**Files:** `plugin/commands/*.md`, `plugin/agents/aisdlc-reviewer.md`, `plugin/bin/aisdlc`, `plugin/hooks/hooks.json` (nếu cần), `plugin/test/plugin-files-test.sh`.

- [ ] **Step 1: Viết test.** Thêm vào `plugin-files-test.sh`:
  - không file nào trong `plugin/commands` và `plugin/agents` khớp regex Java;
  - `cases.md` và `step.md` chứa quy tắc so DB và quy ước tên goose;
  - launcher thiếu binary: hook `pre-tool-use` cho `Read`, `Grep`, `Glob` trong repo AISDLC thì exit 0; `Edit`, `Write`, `Bash` thì exit 2; ngoài repo AISDLC thì exit 0 im lặng.
- [ ] **Step 2:** Chạy `bash plugin/test/plugin-files-test.sh`. Mong đợi FAIL.
- [ ] **Step 3:** Sửa nội dung lệnh:
  - `eval.md` nói build, unit test, Postgres/Kafka qua Docker và bộ case;
  - `step.md` hướng dẫn agent viết Go và migration goose;
  - reviewer dùng tiêu chí Go.

  Sửa launcher: đọc `tool_name` từ stdin JSON giống cách đang đọc `cwd`.
- [ ] **Step 4:** Chạy lại test. Mong đợi PASS. Chạy thêm `make check`.
- [ ] **Step 5:** Commit `feat(plugin): lệnh và skill cho Go, launcher cho đọc khi thiếu binary`.

### Task 3: Lời giải e2e bằng Go và dịch vụ mẫu đầy đủ

**Files:**
- `e2e/fixture-service/**`: thêm những gì lời giải cần, ví dụ bảng `account` đã có, endpoint fee;
- `e2e/solutions/{PAY-1,FIX-2,FEE-3,flow}/**`: viết lại bằng Go:
  - `PAY-1/S1/internal/limit/web/handler.go`;
  - `S2/internal/limit/service.go`;
  - `S2/db/migrations/00002_daily_limit.sql`;
  - `PAY-1/db/00002_daily_limit.sql`;
  - `resubmit/design.md`;
  - `risk/cases-no-repeat.yaml`;
  - `FIX-2/S1/internal/fee/fee.go`;
  - các file `cases.yaml`, `design.yaml`, `brd.md` và `review-agent.json` sửa đường dẫn Go.

**Interfaces:** cấu trúc thư mục giữ như Java để `run-e2e.sh` (Task 4) chép theo cùng cách.
- [ ] **Step 1: Viết test** `internal/harness/e2e_solutions_test.go`, cần Docker. Với mỗi tính năng PAY-1, FIX-2, FEE-3:
  1. chép fixture vào thư mục tạm;
  2. áp lời giải theo thứ tự bước;
  3. `Harness.Run`, mode `cases`, trên `cases.yaml` của lời giải.

  Kỳ vọng: mọi case PASSED sau bước cuối. Trước khi áp code của bước, case của bước đó phải fail, đúng như `submit cases` đòi.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Viết lời giải. Chỉ nội dung tương đương nghiệp vụ Java (hạn mức ngày, phí 0,15%), không đổi kịch bản.
- [ ] **Step 4:** Chạy `go test ./internal/harness/ -run E2ESolutions` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(e2e): lời giải PAY-1, FIX-2, FEE-3 bằng Go`.

### Task 4: `e2e/run-e2e.sh` cho Go — phần local, flow, session, web và Playwright

**Files:**
- `e2e/run-e2e.sh`: viết lại từ bản Java, giữ đủ các `step` dương và âm (danh sách step của bản Java là checklist);
- `e2e/web-*.mjs`, `e2e/screenshots.mjs`, `e2e/web-check.sh`: trỏ tới server Go;
- `e2e/package.json` (Playwright);
- `Makefile` thêm target `e2e` và `e2e-web`.

- [ ] **Step 1:** Port từng nhóm step của Java:
  - build `aisdlc`, `aisdlc-web` và plugin dist;
  - repo dịch vụ từ `e2e/fixture-service`;
  - PAY-1 FULL;
  - FIX-2 LITE;
  - phần web: server Go chạy `aisdlc-web` với Postgres `docker run`, token, `web login`, `policy pull`, `migrate`, duyệt qua Playwright, trả lại và nộp lại, cache bị sửa, chỉ dẫn, tạm dừng, độ trễ hook, accept S1, cho phép file, lệnh hết hiệu lực, web chết, review, `eval --ci` với token ci;
  - `flow-service`: tự duyệt, nâng mức;
  - session song song;
  - âm tính: `CASES_LOCKED`, hook chặn `cases.yaml`, thiếu console thì exit 3...

  Thêm ba bước Review Focus: eval với `DOCKER_HOST` sai; fixture bỏ qua SIGTERM thì coverage N/A; chạy lại hai lần.
- [ ] **Step 2:** Chạy `make e2e`. Mong đợi: in `== xong`, exit 0, log ở `e2e/out/`. Bước nào lệch bản Java thì ghi trong report kèm lý do.
- [ ] **Step 3:** Chạy `make e2e` lần hai liền. Mong đợi vẫn PASS.
- [ ] **Step 4:** Commit `feat(e2e): kịch bản e2e trên bản Go`.

### Task 5: CI của repo AISDLC

**Files:** `.gitlab-ci.yml`, `Makefile` (target `ci`).
- [ ] **Step 1:** Viết `.gitlab-ci.yml` với các stage:
  - `check`: `make check` có `docker:dind` cho test testcontainers, `AISDLC_SKIP_DOCKER` không đặt;
  - `ui`: `make ui ui-test`;
  - `build`: `make plugin-dist build-web`, lưu artifact;
  - `docker`: build `Dockerfile.web`, chỉ trên tag hoặc main;
  - `e2e`: `make e2e` chạy tay hoặc theo lịch, chạy hai lần.

  Test `internal/ci_test.go` (hoặc script): parse YAML, các job tồn tại, job dùng testcontainers có `DOCKER_HOST` và `TESTCONTAINERS_HOST_OVERRIDE` hợp lệ cho dind.
- [ ] **Step 2:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 3:** Commit `ci: pipeline GitLab cho repo AISDLC`.

### Task 6: Tài liệu tiếng Việt

**Files:**
- `README.md`;
- `docs/huong-dan-su-dung.md`: chép từ Java rồi sửa cho Go;
- `docs/van-hanh-web.md`: env, Dockerfile, khóa ký dùng chung giữa replica, `pool_max_conns`, purge 03:00 theo `TZ`, cookie `Secure` sau proxy TLS;
- `docs/images/` nếu cần;
- `docs/docs-check.sh`.

- [ ] **Step 1: Viết `docs/docs-check.sh`.** Nó trích mọi khối ```` ```bash ```` có dòng `# chạy-được` trong `huong-dan-su-dung.md`, rồi chạy trên repo tạm dựng từ `e2e/fixture-service`, dùng binary vừa build và `HOME` tạm. Khối nào lỗi thì fail.
- [ ] **Step 2:** Viết tài liệu, gồm:
  - cài plugin, `setup`;
  - luồng FULL và LITE;
  - quy tắc case và so DB;
  - eval;
  - chế độ web: login, duyệt, điều khiển;
  - nhiều tính năng song song theo session;
  - duyệt trong Claude Code;
  - giới hạn đã biết, ví dụ hook Bash là heuristic (danh sách đường vòng đã biết từ giai đoạn 2b).
- [ ] **Step 3:** Chạy `bash docs/docs-check.sh` và grep Java. Mong đợi PASS.
- [ ] **Step 4:** Commit `docs: tài liệu tiếng Việt cho bản Go`.

### Task 7: Dọn cuối

**Files:** `internal/cli/**` (chuỗi help/usage của cobra), `docs/superpowers/deferred-tests.md`, các TODO còn sót.
- [ ] **Step 1:** Việt hóa help và usage của cobra: `Usage:`, `Flags:`, `help for`... qua `SetUsageTemplate`/`SetHelpTemplate`. Test `TestHelpIsVietnamese`.
- [ ] **Step 2:** `grep -rn 'giai đoạn [345]\|TODO' internal cmd` không còn placeholder nào của giai đoạn đã xong. `deferred-tests.md` chỉ còn mục "Không port vì thiết kế Go khác".
- [ ] **Step 3:** Chạy `go test ./...`, `make check`, rồi `make e2e`. Mong đợi PASS.
- [ ] **Step 4:** Commit `chore: help tiếng Việt, dọn placeholder`.
