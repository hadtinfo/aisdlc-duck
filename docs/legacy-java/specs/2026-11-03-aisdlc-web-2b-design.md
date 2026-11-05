# AISDLC Web — Giai đoạn 2b: duyệt trên web, tạm dừng và chỉ dẫn

Trạng thái: **2b-1 (duyệt trên web) đã xong** trên nhánh `aisdlc-web-2b1` (2026-11-03); kế hoạch `docs/superpowers/plans/2026-11-03-aisdlc-web-2b1.md`, hướng dẫn ở `docs/README.vi.md` mục 10. **2b-2 (tạm dừng, chỉ dẫn, accept bước / cho phép file trên web, diff giữa các lần nộp) đã xong** trên nhánh `aisdlc-web-2b2` (2026-11-04), chưa merge vào `main`; kế hoạch `docs/superpowers/plans/2026-11-04-aisdlc-web-2b2.md`, hướng dẫn ở `docs/README.vi.md` mục 11.

Spec gốc: `2026-11-02-aisdlc-design.md` (§5, §7, §7b, §7c.2). Nền tảng: 2a (`2026-11-03-aisdlc-web-2a-design.md`) đã có snapshot/event một chiều, outbox, tiến trình gửi nền và ba màn hình chỉ xem.

## 1. Mục tiêu

1. **Web là nơi lưu chính bản ghi duyệt** (§7, §7b). Bản ghi duyệt nằm ngoài repo, nên agent không thể tự sửa file để "duyệt", và CI hỏi web thay vì tin `gates.json`.
2. Dev **duyệt / trả lại** cổng, **accept** bước và **cho phép file ngoài phạm vi** ngay trên trang B, kèm comment.
3. Dev **tạm dừng** agent hoặc **gửi chỉ dẫn** giữa một bước (màn C). Agent nhận ở lần gọi tool kế tiếp.
4. Đăng nhập: **giữ token cá nhân** (công ty chưa có SSO/LDAP). Xem §10. **[Quyết định của người dùng]**

**Không phải mục tiêu:** web khởi chạy agent; Jira/GitLab (2c); duyệt nhiều người (vẫn là một dev chủ tính năng).

**Phản mục tiêu:**
- Web chết làm dev **mất** những gì đã duyệt.
- Agent tìm được đường tự duyệt qua web, ví dụ dùng token của dev mà CLI đang lưu.
- Thêm thao tác thừa. Duyệt trên web không được nhiều bước hơn gõ lệnh ở terminal.

## 2. Nguồn lưu cổng **[Lựa chọn của Claude]**

- `aisdlc/config.yaml` có thêm `approvals: local | web`. Mặc định là `local`, tức giữ nguyên hành vi giai đoạn 1. Repo chuyển sang `web` bằng một MR cấu hình. Theo quy tắc giai đoạn 1, MR này không qua được job eval, nên phải tạm cho phép job fail.
- `WebGateStore implements GateStore`:
  - `submission/approval` đọc từ web.
  - `saveSubmission` gửi lên web **đồng bộ**, timeout 5 giây. Lỗi thì lệnh `submit` báo lỗi, không ghi gì.
  - `saveApproval` và `clear` **không** gọi được từ CLI ở chế độ web. Duyệt chỉ làm trên web.
- **Lúc nộp:** web lưu submission kèm **toàn văn các artifact** của cổng tại thời điểm nộp, cùng hash tính lại phía server từ chính nội dung đó. Hash khác hash CLI gửi thì từ chối. Trang B hiển thị đúng bản đã nộp, không phải bản snapshot mới nhất.
- **Lúc duyệt:** web ghi `Approval(gate, hash, approver, at)` gắn với submission. Không thể duyệt một hash chưa từng được nộp.
- **Cache cục bộ** `.aisdlc/web/gates/<KEY>.json`: bản sao các approval, mỗi bản có **chữ ký Ed25519 của server** (khóa riêng chỉ nằm trên server). Hook dùng cache này khi web không truy cập được.
  - Approval trong cache chỉ có hiệu lực khi chữ ký hợp lệ và hash khớp nội dung hiện tại.
  - Hook kiểm chữ ký bằng **khóa công khai** của server, lấy về khi chạy `aisdlc web login` và lưu ở `~/.config/aisdlc/`, ngoài repo. Agent ghi đè cache cũng không tạo được chữ ký hợp lệ.
- **Web chết** (§7b):
  - Cổng đã duyệt (cache hợp lệ) vẫn dùng được.
  - Không nộp hoặc duyệt mới được.
  - Sửa code bị chặn nếu cổng chưa từng được duyệt.
  - Event vẫn xếp hàng như 2a.
- **Chuyển từ local sang web:** lệnh `aisdlc web migrate` gửi các approval có sẵn trong `gates.json` lên web. Chúng được đánh dấu "nhập từ local" kèm người và thời điểm gốc, và dev chủ tính năng phải xác nhận một lần trên web.

## 3. Thao tác trên web

- **Ai được thao tác:** chủ tính năng (owner) và người có vai trò `lead`. Người khác chỉ xem. Quyền được kiểm phía server. Mọi thao tác ghi lại người làm, thời điểm và địa chỉ IP vào bảng `audit`, không sửa và không xóa.
- **Trang B:**
  - Nút **Duyệt** nằm ngay dưới review card (`2026-11-03-aisdlc-review-design.md` §8). Nút hiện hash rút gọn và yêu cầu bấm xác nhận lần hai, tương đương việc gõ lại feature key. Còn cờ đỏ chưa xem thì bước xác nhận liệt kê mã cờ. Bản ghi duyệt lưu kèm mã cờ lúc duyệt và các giả định dev đã tick.
  - Nút **Trả lại**: bắt buộc nhập comment. Comment được ghi vào bảng `feedback` và trở thành chỉ dẫn cho agent ở lượt kế tiếp, theo cùng cơ chế ở §4.
  - Nút **Mở lại** (reopen).
  - Ở bước Review: nút Duyệt chỉ bật khi eval mới nhất ĐẠT, khớp cây code hiện tại (`evaluatedTree`) và mọi bước đã được accept. Đây là các quy tắc của giai đoạn 1, nay được kiểm phía server từ snapshot.
- **Bước:** nút **Accept Sx** (chỉ khi bước đang `AWAITING_DEV`) và **Cho phép file ngoài phạm vi** (từng file).
  - Trạng thái bước vẫn nằm trong `steps.json` ở repo, nên web không ghi thẳng vào đó. Web tạo một **lệnh chờ áp dụng** (`pending action`) và ký nó.
  - CLI hoặc hook áp dụng lệnh ở lần chạy kế tiếp: kiểm chữ ký, ghi vào `steps.json` / `scope-extra.json`, rồi báo lại web "đã áp dụng".
  - Agent không tạo được lệnh có chữ ký hợp lệ.
- **Duyệt bằng terminal ở chế độ web:** **giữ cả hai đường**, xem §10. **[Quyết định của người dùng]**

## 4. Tạm dừng và chỉ dẫn

- **Màn C có thêm:**
  - Nút **Tạm dừng** / **Tiếp tục**.
  - Ô **Gửi chỉ dẫn**, tối đa 2000 ký tự. Gửi đi rồi thì không sửa được.
- **Đường về máy dev:** tiến trình gửi nền của 2a (chạy sau mỗi lần dùng tool) cũng tải **trạng thái điều khiển** của tính năng về `.aisdlc/web/control/<KEY>.json`. File này gồm cờ tạm dừng, danh sách chỉ dẫn chưa giao và các lệnh chờ áp dụng, tất cả có chữ ký. Hook chỉ đọc file cục bộ này nên vẫn đạt mục tiêu dưới 300 ms. Độ trễ thực tế khoảng một lần gọi tool.
- **Tạm dừng:**
  - PreToolUse **chặn** mọi tool ghi với lý do "Dev đã tạm dừng tại web: <tên>. Dừng lại và chờ."
  - Hook Stop **không** chặn việc dừng, để agent dừng được.
  - Khi dev bấm Tiếp tục, cờ được gỡ ở lần tải kế tiếp.
- **Chỉ dẫn:** hook PostToolUse trả `additionalContext` chứa các chỉ dẫn chưa giao, theo dạng `Chỉ dẫn của dev (<giờ>): …`, rồi đánh dấu đã giao. Web hiện "đã giao lúc …".
  - Builder phải xác minh `additionalContext` của PostToolUse trên phiên bản Claude Code mà team đang dùng. Nếu không hỗ trợ, dùng hook `UserPromptSubmit` hoặc lý do deny của PreToolUse.
- Comment khi trả lại cổng đi theo đúng đường chỉ dẫn này, đồng thời vẫn ghi vào `feedback.md` như giai đoạn 1 để agent đọc lại được.

## 5. CI

- `aisdlc eval --ci` ở chế độ web gọi `GET /api/ci/approvals?repo=&feature=` bằng **token CI** (tài khoản dịch vụ chỉ đọc), rồi so hash approval với nội dung trong MR.
  - Cổng BRD, DESIGN hoặc REVIEW chưa duyệt hay đã đổi sau duyệt chuyển từ **WARN thành FAIL**. Cổng CASES đã là FAIL từ giai đoạn 1.
  - Web không trả lời thì job **fail** với thông báo rõ ràng, có thể chạy lại. Không có chế độ bỏ qua.
- Token CI được lưu trong biến CI được bảo vệ (masked, protected) của GitLab.

## 6. Đăng nhập (đã được §10 thay thế phần SSO/LDAP)

- Nếu công ty có **OIDC/SAML SSO** (Keycloak, Azure AD, ADFS): dùng Spring Security OAuth2 Login và ánh xạ email sang user.
- Nếu chỉ có **AD/LDAP**: dùng form đăng nhập và bind LDAP.
- Trong cả hai trường hợp:
  - Token cá nhân của 2a vẫn dùng cho CLI và hook.
  - Thêm lệnh `aisdlc web login`: mở trình duyệt hoặc dán code, rồi lưu token vào `~/.config/aisdlc/web.json` với quyền 600.
  - Token CLI có **phạm vi** `ingest` và `control:read`. Token này **không** có quyền duyệt, nên agent đọc được token cũng không tự duyệt được.
  - Thao tác duyệt chỉ đi qua phiên trình duyệt đã đăng nhập, có chống CSRF.
- Có trang quản lý token: xem danh sách và thu hồi. Đây cũng là minor còn treo từ 2a.

## 7. Kiểm thử

- `WebGateStore` và cache có chữ ký:
  - giả mạo cache, approval cũ hoặc hash lệch thì bị chặn;
  - web chết có cache hợp lệ thì vẫn sửa code được;
  - web chết không có cache thì bị chặn.
- Quyền:
  - người không phải owner duyệt thì nhận 403;
  - token CLI gọi API duyệt thì nhận 403;
  - thiếu CSRF thì nhận 403.
- Lệnh chờ áp dụng: chữ ký sai thì bị bỏ qua; áp dụng hai lần vẫn chỉ có tác dụng một lần (idempotent).
- Tạm dừng và chỉ dẫn: kiểm độ trễ hook dưới 300 ms trên tiến trình thật; chỉ dẫn được giao đúng một lần.
- E2E: chạy lại luồng PAY-1 với `approvals: web`, duyệt bằng Playwright trên trang B. Có thêm hai đoạn:
  - tắt web giữa chừng: code vẫn sửa được nhờ cache, còn nộp mới thì báo lỗi;
  - tạm dừng: tool ghi bị chặn rồi được mở lại.

## 8. Để builder quyết

Tên bảng và endpoint, định dạng chữ ký, chu kỳ tải trạng thái điều khiển, giao diện xác nhận.

## 9. Chốt sau vòng đọc thử của builder **[Lựa chọn của Claude]**

1. **Gốc tin cậy của chữ ký:**
   - Khóa công khai của server được **ghim theo user** ở `~/.config/aisdlc/web-pubkey` ngay lần `aisdlc web login` đầu tiên.
   - `web login` dùng luồng device code: CLI in mã, dev xác nhận mã trong phiên trình duyệt đã đăng nhập. Agent không tự hoàn tất được.
   - Khi đổi khóa, server phát khóa mới **được ký bằng khóa đang ghim**. CLI chỉ chấp nhận khóa mới có chữ ký hợp lệ như vậy.
   - Hook PreToolUse chặn thêm việc ghi vào `~/.config/aisdlc/**`, giống như đã chặn `.aisdlc/` trong repo.
   - Mô hình đe dọa vẫn theo §7b: agent mắc lỗi thật thà, không cố ý tấn công.
2. **Chế độ duyệt do web quyết:**
   - Chế độ (`local`/`web`) được **ghim trên web theo repo**. Cache có chữ ký mang theo bản ghi chế độ này.
   - Khi web hoặc cache nói `web`, hook và CLI bỏ qua `gates.json`, bỏ qua giá trị `approvals` trong `config.yaml`, và không dùng `gates.json` làm dự phòng.
   - `config.yaml` chỉ dùng lúc khởi tạo. Muốn quay về `local` thì người có vai trò `lead` phải đổi trên web.
3. **Lệnh chờ áp dụng:**
   - Mỗi lệnh mang theo trạng thái nó kỳ vọng, gồm bước và `endTree` lúc dev bấm, và **hết hạn sau 7 ngày**.
   - Lệnh chỉ được áp dụng khi `steps.json` vẫn đúng trạng thái kỳ vọng. Nếu không, web đánh dấu "hết hiệu lực" kèm lý do và dev phải bấm lại.
   - Lệnh được áp dụng ở **mọi** lần CLI hoặc hook chạy, kể cả `aisdlc status` và SessionStart. Dev tự chạy một lệnh là đủ, không cần agent đang chạy.
   - Trong lúc chờ áp dụng, web hiện "đã accept, chờ áp dụng".
4. **Tạm dừng khi mất web:** cờ tạm dừng trong cache **giữ nguyên** cho tới khi tải được trạng thái mới, theo hướng an toàn. Agent có thể ghi thêm tối đa một lần sau khi dev bấm Tạm dừng, vì độ trễ khoảng một lần gọi tool. Mức này chấp nhận được và được nói rõ trên nút.
5. **Duyệt REVIEW gắn với cây code:**
   - Approval của REVIEW ghi thêm `evaluatedTree`.
   - Nếu có commit làm đổi code sau khi duyệt, CI so cây code ở đầu MR với cây đã duyệt và **fail** với lý do "REVIEW đã đổi sau duyệt". Dev chạy lại eval rồi duyệt lại.
   - Sau mỗi lần duyệt lại, mô tả MR (2c) được cập nhật. Trong khoảng chưa duyệt lại, mô tả MR có dòng cảnh báo.
6. **Event mang `sessionId`** (lấy từ `session_id` trong input của hook). Hợp đồng event lên phiên bản 2 với trường này là tùy chọn. Giai đoạn 3 dùng `sessionId` để gán cấu hình cho từng lượt tool.

## 10. Quyết định của người dùng (2026-11-03)

**Đăng nhập:** giữ token cá nhân, chưa làm SSO/LDAP. **[Quyết định của người dùng]**
- Trình duyệt vẫn đăng nhập bằng token như 2a. Phiên trình duyệt có chống CSRF.
- Trang cá nhân trên web cho dev **tự tạo token có phạm vi**, xem danh sách và thu hồi. Hai loại token:
  - `ingest`: quyền `ingest` và `control:read`, dùng cho hook và tiến trình gửi nền.
  - `approve`: quyền duyệt, chỉ dùng cho lệnh dev ở terminal.
- `aisdlc web login` không dùng device code. Dev dán token `ingest` lấy từ trang cá nhân, rồi CLI ghim khóa công khai của server (§9.1).
- Phần SSO/LDAP ở §6 để dành cho sau.

**Duyệt bằng terminal: giữ cả hai đường.** **[Quyết định của người dùng]**
- `aisdlc approve | reject | reopen | step accept | scope allow` ở chế độ web vẫn đòi console thật và gõ lại feature key như giai đoạn 1. Sau đó CLI gọi API duyệt bằng **token `approve`**. Token này được lưu ở `~/.config/aisdlc/approve-token` (quyền 600), tách khỏi `web.json`.
- Hook PreToolUse chặn mọi tool đọc hoặc ghi `~/.config/aisdlc/**`, kể cả Read và Bash. Matcher của PreToolUse được mở rộng thêm `Read|Grep|Glob` cho đúng đường dẫn này.
- Server ghi `kênh: terminal | web` vào bản ghi duyệt và bảng `audit`.
- **Đánh đổi** (ghi lại để bạn biết): đường terminal yếu hơn đường web. Theo mô hình §7b, agent mắc lỗi thật thà, nên chỉ cần hook chặn là đủ. Nếu sau này muốn mức chặt hơn, có thể bỏ token `approve` và chỉ duyệt trên web.
- Trường hợp chưa có token `approve`, CLI in link tới trang B.
