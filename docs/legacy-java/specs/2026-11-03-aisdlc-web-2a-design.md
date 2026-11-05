# AISDLC Web — Giai đoạn 2a: web chỉ xem (bảng, review, theo dõi agent)

Trạng thái: **đã xong** trên nhánh `aisdlc-web-2a` @ `b9e4798` (2026-11-03), chưa merge vào `main`. Trang B được mở rộng theo `2026-11-03-aisdlc-review-design.md` (R2).

Spec gốc: `2026-11-02-aisdlc-design.md` (§7, §7b, §11 mục 5). Tài liệu này chỉ mô tả phần 2a.
2b (duyệt trên web, tạm dừng / chỉ dẫn, web là nơi lưu chính trạng thái cổng) và 2c (Jira, GitLab MR, chỉ số) làm sau.

## 1. Mục tiêu

Dev và cả team nhìn thấy trên trình duyệt, gần thời gian thực, mọi tính năng đang chạy trong các repo dùng AISDLC:
- tính năng đang ở cổng nào, việc gì đang chờ ai;
- nội dung đã nộp và đã duyệt (BRD, sequence, schema, case, báo cáo eval, diff theo bước);
- agent đang sửa file nào, so với phạm vi và ngân sách của bước.

**[Quyết định của người dùng]** Làm 2a trước, trong 2a **duyệt vẫn bằng terminal**. Web không có nút ghi trạng thái.

**Không phải mục tiêu của 2a:** nút Duyệt/Trả lại/Tạm dừng/Chỉ dẫn; Jira, GitLab; xu hướng tầng 2; web khởi chạy agent.

**Phản mục tiêu:**
- Web chết hoặc chậm làm agent hoặc lệnh `aisdlc` chậm hay bị chặn. Trong 2a, web chỉ là nơi nhận bản sao, nên mọi lỗi gửi đều **mở** (bỏ qua, xếp hàng gửi lại).
- Web hiển thị trạng thái khác với repo mà không cho biết. Mỗi thẻ hiện thời điểm đồng bộ cuối.
- Lại là một lớp nội dung dài. Màn hình ưu tiên số liệu, pass/fail, điểm lệch.

## 2. Luồng dữ liệu **[Lựa chọn của Claude]**

Máy dev → web, một chiều, qua HTTP JSON:

1. **Bản chụp tính năng (snapshot):** sau mỗi lệnh `aisdlc` làm đổi trạng thái (`init`, `submit`, `approve`, `reject`, `reopen`, `step start/finish/accept`, `scope allow`, `eval`), CLI gửi toàn bộ trạng thái hiện tại của tính năng: `feature.yaml`, `gates.json`, `steps.json`, `eval/report.json`, `review-agent.json`, nội dung các artifact (`brd.md`, `design.md`, `design.yaml`, `cases.yaml`, migration nháp), và **diff của từng bước** (`git diff startTree endTree`, hoặc tới cây hiện tại nếu bước đang chạy). Web luôn thay bằng bản mới nhất, nên gửi lặp lại không sao.
   - Thứ tự snapshot: xem §5b mục 5.
   - Giới hạn: mỗi file artifact tối đa 256 KB, diff mỗi bước tối đa 512 KB. Vượt thì cắt và đánh dấu `truncated`.
2. **Sự kiện (event):** ghi lịch sử và phục vụ màn C: các lệnh ở trên, cộng **mỗi lần agent sửa file** (hook PostToolUse mới cho Edit/Write/MultiEdit/NotebookEdit, và Bash khi đang có bước chạy), SessionStart, Stop. Mỗi event gồm loại, thời điểm, tính năng, bước, đường dẫn file nếu có, và cờ ngoài phạm vi (tính bằng `ScopeMatcher` có sẵn). Hook PostToolUse **không** chạy git diff toàn bộ. Số dòng của bước chỉ cập nhật ở snapshot.
3. **Gửi không chặn:** CLI và hook ghi bản tin vào hàng đợi `.aisdlc/web/outbox/` (một file JSON mỗi bản tin, ghi nguyên tử), rồi thử gửi với timeout 1 giây kết nối và 2 giây tổng. Gửi được thì xóa file. Lỗi thì giữ lại và gửi bù ở lần gọi kế tiếp, tối đa 20 bản tin mỗi lần để không làm chậm hook. Hàng đợi quá 2000 file thì bỏ event cũ nhất (snapshot của mỗi tính năng chỉ giữ bản mới nhất). Lỗi gửi không bao giờ làm đổi exit code hay quyết định của hook.
4. **Bật/tắt:** không có `~/.config/aisdlc/web.json` (`{"url": "...", "token": "..."}`) hoặc biến môi trường `AISDLC_WEB_URL` / `AISDLC_WEB_TOKEN` thì không gửi gì và không tạo hàng đợi. Giai đoạn 1 vẫn chạy y như cũ.
5. **Định danh repo:** URL remote `origin` sau khi chuẩn hóa (bỏ `.git`, bỏ user, ssh và https về cùng dạng `host/group/name`). Không có remote thì dùng tên thư mục gốc, kèm cảnh báo một dòng ở `aisdlc status`. Nhánh git hiện tại gửi kèm mỗi snapshot.
6. **Lệnh mới:** `aisdlc web status` (cấu hình, số bản tin đang chờ, lần gửi thành công cuối) và `aisdlc web push` (gửi snapshot mọi tính năng trong repo ngay). Agent được chạy cả hai.

`.aisdlc/web/` nằm trong `.aisdlc/` (đã gitignore và đã được hook bảo vệ khỏi agent ghi).

## 3. Máy chủ web **[Lựa chọn của Claude]**

- Module mới `aisdlc-web` trong repo này: Spring Boot 3.3, Java 17, Postgres 16, Flyway. Frontend React 18 + TypeScript + Vite trong `aisdlc-web/ui`, build ra file tĩnh và đóng vào jar (frontend-maven-plugin, Node cố định phiên bản). Dùng `mermaid` để vẽ sequence. Một image Docker, kèm `docker-compose.yml` (web + postgres) để chạy trên máy chủ nội bộ.
- **Đăng nhập:** mỗi dev có một token cá nhân. Admin tạo bằng lệnh `java -jar aisdlc-web.jar user add <login> "<Tên>"`, lệnh in token một lần. DB chỉ lưu hash. Token dùng cho cả API ghi (CLI gửi lên) và đăng nhập trình duyệt (nhập token một lần, lưu vào cookie phiên HttpOnly). Chuyển sang tài khoản công ty (LDAP/SSO) sẽ làm khi team cho biết hệ thống đang dùng.
- **Quyền:** ai đăng nhập cũng xem được mọi thứ. Snapshot và event ghi lại `login` của token đã gửi.
- **Lưu trữ:** bảng `users`, `repos`, `features` (khóa: repo + feature key; snapshot mới nhất dạng JSONB, `capturedAt`, `clientId`, thời điểm đồng bộ, người gửi), `events` (append-only, có chỉ mục theo repo + feature + thời điểm). Giữ event 180 ngày.
- **Cập nhật trực tiếp:** SSE. `/api/stream` cho bảng A (thẻ thay đổi), `/api/features/{id}/stream` cho trang B/C (snapshot mới, event mới). Trình duyệt mất kết nối thì tự nối lại và tải lại dữ liệu đầy đủ.
- **API ghi:** `POST /api/ingest/snapshot`, `POST /api/ingest/events` (theo lô). Header `Authorization: Bearer <token>`. Snapshot bị từ chối (400) nếu sai cấu trúc. Payload tối đa 8 MB.

## 4. Màn hình

**A. Bảng quy trình (trang chính).** Cột: BRD → Thiết kế → Case → Code → Review → Xong. Tính năng luồng nhẹ không đi qua BRD và Thiết kế.
- Cột của thẻ là cổng đầu tiên chưa được duyệt. "Xong" khi cổng REVIEW đã duyệt và không STALE.
- Thẻ hiện: Jira key, tiêu đề, chủ tính năng (người tạo), repo và nhánh, nhãn trạng thái, eval tóm tắt (ĐẠT/KHÔNG ĐẠT, số case pass) và "đồng bộ X phút trước".
  - Nhãn trạng thái: chờ duyệt, bị trả lại, STALE, đang code Sx, chờ accept Sx, eval không đạt.
- Bộ lọc: repo, chủ tính năng, "ẩn đã xong quá 7 ngày".
- **Khung "Việc chờ tôi"** (Warp mục 5): với tính năng của người đang xem, liệt kê:
  - cổng chờ duyệt,
  - bước chờ `step accept`,
  - câu hỏi mở chưa trả lời (dòng `- [ ]` trong mục `Câu hỏi mở`),
  - bước `finish` có case fail hoặc file ngoài phạm vi,
  - eval không đạt,
  - cổng STALE.

  Mỗi mục có lệnh terminal cần chạy, kèm nút copy. Mục tự mất khi snapshot mới cho thấy đã xử lý.

**B. Trang review tính năng.**
- Trái: danh sách cổng và bước kèm trạng thái.
- Giữa: nội dung của mục đang chọn.
  - BRD: markdown.
  - Thiết kế: sequence Mermaid đã render, bảng bước kèm phạm vi và ngân sách, migration nháp, API.
  - Case: bảng gồm id, bước, loại, mô tả, request và kỳ vọng rút gọn, mở ra được để xem đầy đủ.
  - Review: báo cáo eval dạng bảng, diff **nhóm theo bước Sx** (có highlight cú pháp, file ngoài phạm vi tô đỏ), nhận xét của agent review theo nhãn.
- Phải: hash đã nộp và hash đã duyệt, ai duyệt, lúc nào; phát hiện tự động; lịch sử event của cổng. Nếu artifact bị sửa sau duyệt, hiện chỗ lệch.
- Không có nút ghi. Có ô hiện lệnh terminal tương ứng (`aisdlc approve design`…).

**C. Theo dõi agent.** Mở từ thẻ đang ở cột Code.
- Danh sách bước S1…Sn: trạng thái, file và dòng so với ngân sách (thanh tiến độ, vàng khi vượt 1×, đỏ khi vượt 1,5×), case pass/fail của lần `finish` gần nhất.
- Dòng thời gian trực tiếp các file agent vừa sửa, file ngoài phạm vi tô đỏ.
- Phiên Claude Code đang mở hay đã dừng (từ SessionStart/Stop, quá 30 phút không có event thì coi như đã dừng).

Ngôn ngữ giao diện: tiếng Việt. Hỗ trợ sáng/tối, màn hình từ 1280px (màn điện thoại không ưu tiên).

## 5. Kiểm thử

- CLI:
  - test đơn vị cho outbox (ghi nguyên tử, gửi bù, giới hạn, mất mạng không đổi exit code);
  - test chuẩn hóa remote;
  - hook PostToolUse vẫn dưới 300 ms khi web không truy cập được, đo trong test.
- Web:
  - test tích hợp Testcontainers Postgres cho ingest (token sai trả 401, snapshot cũ cùng clientId bị bỏ, payload quá lớn bị từ chối), SSE và tính cột hoặc "việc chờ tôi";
  - Vitest cho các hàm thuần ở frontend.
- E2E: mở rộng `e2e/run-e2e.sh`.
  - Bật web bằng `docker compose`, chạy luồng PAY-1 và FIX-2 như cũ, rồi kiểm qua API rằng thẻ đi đúng cột, có event sửa file, eval hiện ĐẠT.
  - Tắt web giữa chừng, rồi kiểm rằng mọi lệnh vẫn pass và hàng đợi được gửi bù khi bật lại.
  - Một script Playwright chụp ba màn hình để dev xem.

## 5b. Chốt sau vòng đọc thử của builder **[Lựa chọn của Claude]**

1. **Chủ tính năng và "tôi":**
   - `aisdlc init` ghi thêm `owner` (email trong `git config user.email`) vào `feature.yaml`. Trường này không bắt buộc, nên tính năng cũ vẫn đọc được.
   - Lệnh `user add` nhận thêm `--email`.
   - Web tìm chủ tính năng như sau:
     - khớp `owner` với email của user;
     - nếu không khớp, lấy login của snapshot đầu tiên;
     - nếu vẫn không có, hiện email thô.
   - "Việc chờ tôi" là việc của các tính năng do mình làm chủ. Người khác chạy lệnh thì vẫn ghi đúng người gửi vào event.
2. **Cột trên bảng:**
   - Các cột BRD, Thiết kế, Case ứng với cổng đầu tiên chưa ở trạng thái APPROVED, bất kể cổng đó đang chờ duyệt, bị trả lại hay STALE. Luồng nhẹ bắt đầu từ cột Case.
   - Khi CASES đã duyệt mà REVIEW chưa duyệt:
     - còn bước chưa ACCEPTED (kể cả chưa bắt đầu) thì thẻ ở cột **Code**;
     - mọi bước đã ACCEPTED thì thẻ ở cột **Review**.
   - Thẻ ở cột **Xong** khi REVIEW đã APPROVED.
   - Mọi quy tắc trên đều tính ở phía web, từ snapshot.
3. **Hook biết tính năng và bước:**
   - Tính năng hiện tại được xác định giống hệt hook PreToolUse đang làm (`ActiveFeature`, có xử lý trường hợp đổi nhánh).
   - Bước hiện tại là bước có trạng thái `IN_PROGRESS`.
   - Không có tính năng nào thì event vẫn được ghi theo repo, không gắn thẻ.
   - Thêm mục PostToolUse vào `plugin/hooks/hooks.json`.
4. **Jira key** là `FeatureKey`. Dữ liệu Jira để 2c.
5. **Thứ tự snapshot:**
   - Bỏ `seq`. Snapshot mang `capturedAt` (giờ máy dev) và `clientId`, một UUID tạo một lần cho mỗi clone và lưu trong `.aisdlc/web/client-id`.
   - Web nhận snapshot nếu `capturedAt` không cũ hơn bản đang lưu. Bản cũ hơn chỉ bị bỏ khi đến từ cùng `clientId`, vì đó là bản gửi bù muộn.
   - Bản cũ hơn từ clone khác vẫn được nhận và gắn cảnh báo "lệch giờ hoặc nhiều máy".
6. **Repo không có remote:** định danh là `local/<tên thư mục>`. Không gộp khi remote được thêm sau.
7. **Quá 8 MB:** CLI bỏ dần diff của các bước, từ bước cũ nhất, và đánh dấu `truncated`, cho tới khi đủ nhỏ.
8. **E2E:** Docker đã là điều kiện từ giai đoạn 1 nên E2E dùng `docker compose`.

## 6. Để builder quyết

Tên endpoint và bảng chi tiết, cấu trúc JSON snapshot (có `schemaVersion`), thư viện UI (ưu tiên ít phụ thuộc), cách highlight diff, cách chạy SSE trong Spring (MVC `SseEmitter` là đủ cho 4 người).

## 7. Mở cho 2b (chỉ ghi lại, không làm)

Snapshot và event đã mang đủ hash nên 2b chỉ cần thêm `WebGateStore` cài vào `GateStore` có sẵn, nút ghi trên web, và đường hook đọc lệnh tạm dừng và chỉ dẫn.
