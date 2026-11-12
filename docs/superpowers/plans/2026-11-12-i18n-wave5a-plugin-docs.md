# Song ngữ EN/VI — Đợt 5a (plugin và tài liệu) Implementation Plan

**Goal:** Prompt của plugin Claude Code viết bằng tiếng Anh, đọc locale của repo từ dòng `Locale:` của
`aisdlc status` và viết tài liệu/trả lời dev bằng ngôn ngữ đó; tài liệu có mục chọn ngôn ngữ; README nêu ngôn ngữ hỗ
trợ; `docs/docs-check.sh` chạy với cả hai locale; CHANGELOG ghi đợt 2–5.

**Global Constraints:**
- Chỉ sửa `plugin/**` (trừ `plugin/lib`, `plugin/bin`), `docs/**`, `README*.md`, `CHANGELOG.md`, `.claude-plugin/`.
- Mã, khóa JSON/YAML, nhãn verdict (`DAT/CAN_SUA/NGHIEM_TRONG`), mã lỗi, tên cổng giữ nguyên.
- Thân mỗi lệnh ≤ 25 dòng; giữ mọi token test bắt buộc (`inputsHash`, `notMatched`, `override`, `RISK_AGENT_INVALID`,
  `aisdlc:aisdlc-risk-classifier`, năm ID tiêu chí review).
- Mọi điều viết trong tài liệu phải kiểm được trong code (`internal/cli/locale.go`, `internal/web/httpx/locale.go`,
  `internal/web/auth`, migration).

**Review Focus:** prompt không mất ràng buộc nào khi dịch; kiểm tra tiếng Việt trong `plugin-files-test.sh` đổi
sang tiếng Anh nhưng giữ ý; subagent (chỉ có Read/Grep/Glob) biết ngôn ngữ qua dòng `Locale:` do lệnh truyền xuống;
docs-check en thật sự chạy repo `locale: en`.

## Tasks

1. **Prompt plugin** — `plugin/commands/*.md`, `plugin/agents/*.md`, `plugin/.claude-plugin/plugin.json`,
   `.claude-plugin/marketplace.json` (mô tả song ngữ), `plugin/test/plugin-files-test.sh` (grep tiếng Anh, thêm kiểm
   mọi prompt có chỉ dẫn `Locale:` và description song ngữ). Kiểm: `bash plugin/test/plugin-files-test.sh`.
2. **docs-check hai locale** — `docs/docs-check.sh [doc] [vi|en]`; không đối số thì chạy mỗi tài liệu dưới cả hai
   locale (repo en: thêm `locale: en` vào config của bản sao fixture; kiểm `aisdlc status` in `Locale: <loc>`).
   Kiểm: `bash docs/docs-check.sh`.
3. **Mục Language / Ngôn ngữ** — `docs/user-guide.md`, `docs/huong-dan-su-dung.md`; `docs/web-operations.md`,
   `docs/van-hanh-web.md` (migration `users.locale`, `PUT /api/me/locale`, `AISDLC_LOCALE` của server) nếu tài liệu
   liệt kê. README.md / README.vi.md: ngôn ngữ hỗ trợ, bỏ ghi chú "currently in Vietnamese".
4. **CHANGELOG** — Unreleased → Added: đợt 2–5.
5. **Kiểm cuối** — `bash plugin/test/plugin-files-test.sh`, `bash docs/docs-check.sh`,
   `AISDLC_SKIP_DOCKER=1 go test ./...`.
