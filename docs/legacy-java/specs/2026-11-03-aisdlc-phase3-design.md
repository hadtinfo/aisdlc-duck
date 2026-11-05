# AISDLC — Giai đoạn 3: eval tầng 2, bộ bài chuẩn, đề xuất tự cải thiện, Confluence

Spec gốc: `2026-11-02-aisdlc-design.md` (§6 tầng 2, §7 màn D, §11 mục 7–8, Confluence). Nền tảng: 2a, 2b (snapshot/event v2 có `sessionId`, duyệt trên web, điều khiển).

## 0. Thay đổi ngày 2026-11-04 **[Quyết định của người dùng]**

**Bỏ 2c.** Chưa cần Jira và GitLab. Giai đoạn 3 phải **độc lập**: không phụ thuộc Jira, GitLab, Confluence hay bất kỳ công cụ ngoài nào, và chạy đủ chức năng khi không có chúng. Tích hợp sau này phải **cắm thêm được** mà không sửa lõi.

Hệ quả cho spec này **[Lựa chọn của Claude]**:
1. **"Tính năng xong"** là khi cổng REVIEW được duyệt (không chờ MR merge). Tính năng bị bỏ dùng nút **Bỏ** trên trang B, có từ giai đoạn này (§10.2).
2. **Token và chi phí** (vốn ở 2c §5) chuyển vào giai đoạn này, xem §3a.
3. **Đề xuất tự cải thiện** không mở MR. Runner tạo một **nhánh git kèm file patch** trong bản sao repo plugin, và web hiện đề xuất ở màn D. Dev tự mở MR nếu muốn.
4. **Confluence (§6) để sau.**
5. **Điểm cắm cho tích hợp:** web có **webhook ra** tùy chọn. Mỗi sự kiện của AISDLC (cổng được nộp, duyệt, trả lại, mở lại; tính năng xong hoặc bị bỏ; bench chạy xong) được POST một JSON có chữ ký HMAC tới các URL do `lead` cấu hình, có retry. Jira, GitLab hay Confluence sau này là một dịch vụ nhỏ nhận webhook này, không phải code trong lõi. Webhook chết không ảnh hưởng gì khác.

## 1. Mục tiêu

1. **Biết chất lượng agent đổi thế nào theo thời gian và theo cấu hình.** Cấu hình gồm CLAUDE.md, plugin, skill và model.
2. **So sánh trước và sau khi đổi cấu hình bằng bộ bài chuẩn**, thay vì cảm nhận.
3. **Biến lỗi lặp lại thành đề xuất sửa cấu hình.** Đề xuất ở dạng MR để dev review, không tự áp dụng.
4. **Đẩy BRD và thiết kế đã duyệt lên Confluence** cho người ngoài team đọc. Mục này tùy chọn.

**Không phải mục tiêu:** chấm điểm dev; tự áp dụng thay đổi cấu hình; web khởi chạy agent cho tính năng thật.

**Phản mục tiêu:**
- Kết luận dựa trên một lần chạy. Agent không tất định nên mỗi cấu hình phải chạy nhiều lần.
- Sửa bộ bài làm sai lệch các kết quả cũ.
- Đề xuất tự cải thiện sinh ra hàng loạt MR mà không ai đọc.

## 2. Phiên bản cấu hình agent **[Lựa chọn của Claude]**

- Hook SessionStart tính **dấu vân tay cấu hình** (`agentConfig`):
  - hash của CLAUDE.md (repo và user);
  - phiên bản và hash của plugin aisdlc;
  - hash các skill đang bật;
  - tên model (từ input của hook nếu có, không có thì lấy từ settings);
  - phiên bản Claude Code.
- Dấu vân tay được gửi kèm event `SESSION_START`, và snapshot mang dấu vân tay của phiên gần nhất.
- Web lưu bảng `agent_configs`: mỗi dấu vân tay một dòng, kèm nội dung đã đổi (diff ngắn) so với bản trước, để trả lời câu hỏi "phiên bản này khác gì".
- Một tính năng có thể đi qua nhiều cấu hình. Chỉ số tầng 2 gán cho cấu hình **chiếm nhiều lượt tool nhất** trong tính năng, và đánh dấu "pha trộn" khi cấu hình đó chiếm dưới 80%.

## 3. Tầng 2 trên dữ liệu thật — màn D "Chất lượng" **[Lựa chọn của Claude]**

- **Lưu kết quả:** mỗi tính năng khi xong (cổng REVIEW được duyệt, §0) hoặc bị bỏ lưu một dòng `feature_outcomes` gồm:
  - luồng;
  - cấu hình;
  - eval có ĐẠT ở lần đầu không;
  - số lần bị trả lại mỗi cổng;
  - tỉ lệ vượt ngân sách lớn nhất;
  - phân bố nhãn agent review;
  - cycle time;
  - token và chi phí.
- **Màn D:**
  - Xu hướng theo tuần của các chỉ số trên.
  - Một bảng **so sánh hai cấu hình**: chọn A và B, xem từng chỉ số kèm khoảng tin cậy (bootstrap). Khi dưới 5 tính năng mỗi bên thì ghi rõ "chưa đủ dữ liệu".
- Đây là quan sát trên tính năng thật, khác nhau về độ khó, nên chỉ dùng để phát hiện xu hướng. Kết luận "cấu hình B tốt hơn A" phải dựa vào bộ bài chuẩn ở §4.

## 3a. Token và chi phí (chuyển từ 2c §5) **[Lựa chọn của Claude]**

- Hook Stop có `transcript_path`. CLI đọc phần `usage` (input, output, cache) của các lượt kể từ lần đọc trước, rồi gửi event `TOKEN_USAGE` gồm số token, model và `sessionId`. **Không gửi nội dung transcript.**
- Chi phí tính bằng bảng giá cấu hình trên web.
- Builder xác minh định dạng transcript của Claude Code đang dùng (2.1.318). Không đọc được thì cột này để trống, không chặn gì.

## 4. Bộ bài chuẩn (benchmark) **[Lựa chọn của Claude]**

- **Repo riêng `aisdlc-bench`**, gồm 6–10 bài. Mỗi bài có:
  - một commit khởi đầu của một service mẫu (dùng fixture Spring Boot sẵn có, mở rộng thêm);
  - BRD, thiết kế và bộ case **đã duyệt** (hash cố định);
  - mô tả ngắn.
- Các bài trải đều: luồng đầy đủ và luồng nhẹ; DB, WireMock, Kafka; vài bài có bẫy như sửa ngoài phạm vi hấp dẫn hoặc case biên.
- **Mỗi bài có phiên bản.** Sửa bài thì tăng phiên bản, và kết quả chỉ được so trong cùng phiên bản bài.
- **Runner `aisdlc bench run`** chạy trên một máy runner riêng của team, **không phải web**:
  - Chạy Claude Code ở chế độ headless (`claude -p`) với plugin và cấu hình cần thử, trong container Docker sạch cho mỗi lần chạy.
  - Chạy mỗi bài **K lần**, mặc định K=5, cho mỗi cấu hình.
  - Cổng của dev được thay bằng **chế độ bench**: bộ case đã khóa sẵn, và lệnh `step accept` được runner tự gọi khi bước xong mà case của bước đó pass.
  - Mỗi lần chạy có giới hạn thời gian và token; vượt giới hạn thì tính là thất bại.
- **Ghi lại** mỗi lần chạy: đạt hay không (eval tầng 1), thời gian, token, chi phí, số lần vượt ngân sách và vi phạm phạm vi, và hash toàn bộ đầu vào (bài, cấu hình, phiên bản runner). Kết quả được gửi lên web vào bảng `bench_runs`.
- **Quy tắc "mỗi lần đổi một thứ":** khi so hai cấu hình, runner so dấu vân tay. Nếu khác ở nhiều hơn một thành phần thì cảnh báo và yêu cầu `--allow-multi`.
- **Màn D có thêm tab Benchmark:** tỉ lệ đạt theo bài và theo cấu hình, khoảng tin cậy, chi phí trung bình.
- **Chi phí:** 10 bài × 5 lần × 2 cấu hình là 100 lần chạy agent cho mỗi lần so sánh.
- **Máy chạy:** một **máy runner riêng** trong mạng nội bộ, có Docker và Claude Code. **[Quyết định của người dùng]**
- Ngân sách token mỗi tháng là một tham số cấu hình của runner (`bench.monthlyTokenBudget`). Runner từ chối chạy khi lần chạy dự kiến làm vượt ngân sách. **[Chờ người dùng: con số cụ thể, có thể chốt lúc triển khai]**

## 5. Đề xuất tự cải thiện **[Lựa chọn của Claude]**

- **Job hằng tuần** trên runner (không phải web, vì cần gọi Claude):
  - Gom vấn đề lặp lại trong 4 tuần gần nhất:
    - cùng tiêu chí agent review nhận CẦN_SỬA hoặc NGHIÊM_TRỌNG ở ít nhất 3 tính năng;
    - cùng cổng bị trả lại với lý do giống nhau, gom bằng embedding hoặc từ khóa;
    - cùng hard check hay fail.
  - Với mỗi cụm đủ ngưỡng, gọi Claude (headless) để soạn **một** thay đổi nhỏ cho CLAUDE.md, command hoặc checklist của agent review, kèm lý do và các tính năng làm bằng chứng.
  - Tạo **một đề xuất mỗi tuần** cho repo plugin, gồm tối đa 3 thay đổi: một nhánh git `aisdlc-proposal/<tuần>` trong bản sao repo plugin trên runner cộng một file patch, gửi lên web (§0.3).
- Đề xuất ghi rõ lý do và bằng chứng, kèm gợi ý chạy bộ bài chuẩn trước và sau khi áp dụng. Dev xem ở màn D rồi tự áp dụng hoặc tự mở MR. **Không bao giờ tự áp dụng.**
- Giới hạn chống spam: tuần nào không có cụm đủ ngưỡng thì không tạo đề xuất; đề xuất bị đánh dấu "bỏ" hai lần thì cụm đó bị tắt.

## 6. Confluence (tùy chọn) — **để sau (§0.4)**; khi làm thì là dịch vụ nhận webhook ra, không nằm trong lõi

- Khi BRD hoặc DESIGN được duyệt, web tạo job đẩy trang lên Confluence:
  - một trang cho mỗi tính năng, tiêu đề `<KEY> <tiêu đề>`, nằm dưới trang cha cấu hình được;
  - hai mục BRD và Thiết kế;
  - sequence Mermaid được render thành ảnh SVG phía server rồi đính kèm;
  - bảng case đặt trong khối "Chi tiết" thu gọn;
  - cuối trang có dòng "Bản đã duyệt lúc …, hash …" và link trang B.
- Duyệt lại thì cập nhật đúng trang đó, không tạo trang mới. Trang chỉ để đọc: sửa trên Confluence không đồng bộ ngược và sẽ bị ghi đè ở lần duyệt sau.
- Dùng cơ chế job và retry của 2c. Confluence chết không ảnh hưởng gì khác (§8).

## 7. Ràng buộc

- Benchmark và job đề xuất gửi mã nguồn mẫu tới Claude. Đây là việc team đã chấp nhận (§8 spec gốc), nhưng bài chuẩn chỉ dùng **service mẫu**, không dùng mã nguồn sản phẩm.
- Chạy được khi web tạm chết: runner xếp hàng kết quả như outbox của 2a.

## 8. Kiểm thử

- Dấu vân tay: đổi một byte trong CLAUDE.md thì ra dấu vân tay mới; đổi thứ tự skill thì dấu vân tay không đổi.
- `feature_outcomes`: tính từ một tập event cố định; kiểm trường hợp "pha trộn".
- Runner: một bài giả với agent giả (script ghi file theo kịch bản) để kiểm ba trường hợp:
  - chế độ bench tự accept bước;
  - vượt giới hạn thời gian thì tính là thất bại;
  - đổi nhiều thành phần cấu hình thì có cảnh báo.
- Job đề xuất: dữ liệu cụm cố định thì ra đúng một đề xuất (nhánh + patch); không đủ ngưỡng thì không có đề xuất.
- Webhook ra: chữ ký HMAC đúng; đích chết thì retry và không ảnh hưởng việc duyệt.
- Token: đọc `usage` từ transcript mẫu, kể cả file thiếu hoặc lạ.

## 9. Để builder quyết

Danh sách bài cụ thể, cách gom cụm, mẫu prompt soạn đề xuất, cách render Mermaid phía server, bố cục màn D.

## 10. Chốt sau vòng đọc thử của builder **[Lựa chọn của Claude]**

1. **Gán cấu hình cho từng lượt tool:** ghép theo `sessionId`, có từ hợp đồng event v2 của 2b. Mỗi phiên lấy dấu vân tay từ `SESSION_START` của chính phiên đó. Lượt tool không xác định được phiên thì không tính.
2. **Tính năng bị bỏ:** nút **Bỏ** trên trang B (chủ tính năng hoặc `lead`, bắt buộc lý do, phiên + CSRF như 2b), làm trong giai đoạn này vì 2c bị bỏ. Không tự suy ra từ việc tính năng lâu không có hoạt động.
3. **Chế độ bench:**
   - Agent chạy với **plugin aisdlc thật**, repo bài đặt `approvals: bench`.
   - Approval của các cổng đã duyệt sẵn được ký bằng **khóa bench** do runner giữ. Khóa riêng nằm ngoài container của agent, container chỉ có khóa công khai.
   - Lệnh `step accept` do runner gọi **từ ngoài container**, ghi vào `steps.json` qua volume. Agent không gọi được, vì hook vẫn chặn lệnh chỉ dev như ở giai đoạn 1.
   - Bench không dùng web để duyệt. Web chỉ nhận kết quả của bench.
4. **Đề xuất tự cải thiện** chỉ nhắm vào **repo plugin**, tức cấu hình chung cả team: template CLAUDE.md, các command, checklist của agent review. Không sửa CLAUDE.md của repo dịch vụ.
   - Runner chỉ tạo nhánh và patch trong bản sao cục bộ của repo plugin (§0.3), không push và không cần tài khoản GitLab.
