# AISDLC Web — Giai đoạn 2c: tích hợp Jira, GitLab và chỉ số theo dõi

Spec gốc: `2026-11-02-aisdlc-design.md` (§5 bước 0/5/6, §7 Jira/GitLab, §8, §11 mục 6). Nền tảng: 2a, 2b.

## 1. Mục tiêu

1. **Jira:**
   - Lấy tiêu đề và mô tả ticket khi `aisdlc init`. Phần kiểm "ticket tồn tại" của giai đoạn 1 hiện đang bị bỏ qua vì chưa cấu hình Jira.
   - Chuyển trạng thái ticket khi qua cổng, theo bảng ánh xạ cấu hình được.
2. **GitLab:**
   - Khi cổng REVIEW được duyệt, tạo hoặc cập nhật MR với mô tả là báo cáo eval tóm tắt và link tới trang B.
   - Biết khi MR được merge để đánh dấu tính năng **Xong**.
3. **Chỉ số theo dõi** (§11 mục 6) trên một trang mới:
   - thời gian mỗi cổng và mỗi bước;
   - số lần bị trả lại mỗi cổng;
   - độ vượt ngân sách;
   - chi phí token mỗi tính năng, nếu lấy được.

**Không phải mục tiêu:** đồng bộ hai chiều (sửa ticket trên Jira không làm đổi gì trong AISDLC); tạo ticket; Confluence (giai đoạn 3).

**Phản mục tiêu:**
- Web trở thành nơi nhập liệu thứ tư, tức có form trùng với Jira.
- Jira hoặc GitLab chết làm hỏng việc duyệt (§8).
- Chỉ số bị dùng để chấm điểm cá nhân dev. Trang chỉ hiện theo tính năng và theo team, **không** có bảng xếp hạng người.

## 2. Thông tin xác thực **[Quyết định của người dùng: Jira và GitLab tự host trong mạng nội bộ; ghi bằng PAT của dev]**

- Đề xuất mặc định theo §7b: **thao tác ghi đứng tên dev**.
  - Mỗi dev nhập Personal Access Token của Jira và GitLab một lần ở trang cá nhân trên web.
  - Server mã hóa token bằng AES-GCM, khóa lấy từ biến môi trường hoặc file secret của máy chủ, không nằm trong DB.
- **Thao tác đọc** (lấy tiêu đề ticket, kiểm MR đã merge) dùng **tài khoản dịch vụ chỉ đọc** nếu có, không có thì dùng token của chủ tính năng.
- Token không bao giờ trả về trình duyệt và không ghi log.

## 3. Jira **[Lựa chọn của Claude]**

- **Lúc init:** CLI gọi `GET /api/jira/issue/<KEY>` trên web, web gọi Jira rồi trả về tiêu đề và mô tả rút gọn. Mô tả được dùng làm đầu vào cho bước phân loại (triage) và cho nháp BRD.
  - Web hoặc Jira không trả lời thì init vẫn chạy, kèm cảnh báo như giai đoạn 1.
- **Bảng ánh xạ** cấu hình trên web theo từng project Jira, chỉ người có vai trò `lead` sửa được, ví dụ:
  - BRD duyệt → "In Analysis";
  - CASES duyệt → "In Progress";
  - REVIEW duyệt → "In Review";
  - MR merge → "Done".
  - Tên transition ghi đúng như trong workflow Jira. Ô nào để trống thì không chuyển.
- **Cách gửi:** mỗi lần qua cổng tạo một job vào bảng `outbound_jobs` trên web. Một worker xử lý với retry (1, 5, 30 phút rồi mỗi giờ, tối đa 24 giờ). Thất bại vĩnh viễn thì hiện trong "Việc chờ tôi" của chủ tính năng kèm lý do, ví dụ transition không tồn tại hoặc token hết hạn.
- Thêm comment lên ticket (một dòng kèm link trang B) mỗi khi qua cổng. Có thể tắt theo từng project.

## 4. GitLab **[Lựa chọn của Claude]**

- **Khi cổng REVIEW được duyệt** (trên web, từ 2b), web tạo job `gitlab.mr.upsert`:
  - Nhánh nguồn lấy từ snapshot (`repo.branch`), nhánh đích là `baseRef` trong `config.yaml`.
  - Đã có MR mở cho nhánh này thì cập nhật, chưa có thì tạo mới.
  - Tiêu đề MR: `<KEY> <tiêu đề>`.
  - Mô tả MR: báo cáo eval dạng markdown (đã giới hạn 40 dòng từ giai đoạn 1), tóm tắt agent review và link trang B. Phần do AISDLC sinh nằm giữa hai marker `<!-- aisdlc:start -->` và `<!-- aisdlc:end -->`, nên phần dev viết tay ngoài marker được giữ nguyên.
  - Label `aisdlc`.
- **Nhánh chưa được push** thì job báo trong "Việc chờ tôi": "Push nhánh `<branch>` rồi bấm Thử lại". Web không push thay dev.
- **Biết MR đã merge:** ưu tiên webhook GitLab (`Merge request events`, có secret token) gửi về `POST /api/gitlab/webhook`. Không cấu hình được webhook thì web hỏi lại mỗi 5 phút các MR đang mở do AISDLC tạo.
  - Khi MR merge: tính năng sang cột **Xong (đã merge)** và tạo job chuyển Jira sang "Done".
- **CI:**
  - Job `aisdlc-eval` gửi báo cáo `--ci` lên web bằng token CI (`POST /api/ci/report`).
  - Trang B hiện kết quả CI cạnh kết quả eval ở máy dev, và cảnh báo khi hai bên khác nhau.
- **Repo GitLab** khớp với `repo.id` của 2a (chuẩn hóa `host/group/name`). Cấu hình URL GitLab và project id được tìm tự động qua API theo đường dẫn.

## 5. Chỉ số theo dõi **[Lựa chọn của Claude]**

- **Nguồn:** event và snapshot đã có từ 2a, cộng event mới từ 2b (duyệt, trả lại, tạm dừng) và từ 2c (MR merge).
- **Trang "Chỉ số"** (màn E), lọc theo repo, khoảng thời gian và luồng (đầy đủ / nhẹ):
  - **Cycle time:** thời gian từ init tới merge; thời gian nằm ở từng cổng (từ lúc nộp tới lúc duyệt) và ở từng bước (từ start tới accept). Hiện trung vị và p90.
  - **Bị trả lại:** số lần trả lại mỗi cổng, kèm tỉ lệ trên số tính năng.
  - **Ngân sách:** phân bố tỉ lệ file và dòng thực tế so với ngân sách mỗi bước, kèm tỉ lệ bước vượt 1,5×.
  - **Eval:** tỉ lệ eval ĐẠT ở lần chạy đầu; các hard check hay fail nhất.
  - **Token:** hook Stop có `transcript_path`. CLI đọc phần `usage` trong transcript (input, output, cache) của các lượt kể từ lần đọc trước, rồi gửi event `TOKEN_USAGE` gồm số token và model. **Không gửi nội dung transcript.** Chi phí tính bằng bảng giá cấu hình trên web.
    - Builder phải xác minh định dạng transcript của phiên bản Claude Code đang dùng. Không đọc được thì cột này để trống, không chặn gì.
- Biểu đồ tuân theo hướng dẫn dataviz. Mọi số liệu tải được dưới dạng CSV.

## 6. Kiểm thử

- Jira và GitLab giả lập bằng WireMock trong test tích hợp của web:
  - init lấy được tiêu đề;
  - qua cổng thì có job chuyển trạng thái;
  - transition sai thì vào "Việc chờ tôi";
  - Jira chết thì duyệt vẫn chạy và job được retry;
  - MR được tạo, rồi cập nhật mà không đè phần mô tả ngoài marker;
  - webhook sai secret thì nhận 401;
  - polling phát hiện MR đã merge.
- Đọc token từ transcript mẫu, có cả trường hợp file thiếu hoặc lạ.
- Chỉ số: kiểm các phép tính trung vị và p90 trên tập event cố định.
- E2E: GitLab và Jira giả (WireMock trong compose) cho luồng PAY-1 đi tới "đã merge".

## 7. Để builder quyết

Cấu trúc bảng job, khoảng retry chi tiết, cách hiển thị biểu đồ, bảng giá token mặc định.

## 8. Chốt sau vòng đọc thử của builder **[Lựa chọn của Claude]**

1. **Mở lại REVIEW sau khi đã có MR:**
   - MR được chuyển sang Draft (tiền tố `Draft:`) và nhận một comment kèm lý do.
   - Bảng ánh xạ Jira có thêm ô tùy chọn "REVIEW mở lại → <transition>". Ô này trống thì không chuyển trạng thái ticket.
   - Đây là phản ứng một chiều theo sự kiện của AISDLC. Thay đổi trên Jira vẫn không đồng bộ ngược về AISDLC.
2. **Token của ai:**
   - Thao tác ghi dùng PAT của **người bấm**. Người đó không có PAT thì dùng PAT của chủ tính năng.
   - Không có PAT nào, hoặc PAT hết hạn: thao tác duyệt **vẫn thành công**. Job lỗi được đưa vào "Việc chờ tôi" của người bấm, kèm nút Thử lại.
3. **Phạm vi quyền:**
   - Tài khoản dịch vụ: GitLab `read_api`, Jira chỉ quyền xem.
   - Webhook dùng secret riêng.
   - Token CI có hai phạm vi `ci:report` và `ci:approvals`, không có quyền nào khác.
4. **Báo cáo CI lệch với kết quả ở máy dev:** hiện cảnh báo trên trang B và thêm một mục "Việc chờ tôi" ("CI và máy dev khác nhau ở <check>"). Cảnh báo này không chặn. Việc chặn merge vẫn do job CI.
5. **Bỏ tính năng:** chủ tính năng hoặc người có vai trò `lead` bấm **Bỏ** trên trang B, bắt buộc kèm lý do. Đây là một nút, không phải form nhập liệu. Tính năng bị bỏ chuyển sang cột Xong với nhãn "đã bỏ".
