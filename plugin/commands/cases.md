---
description: Soạn cases.yaml cụ thể và nộp cổng case
argument-hint: "[ghi chú thêm]"
---
Soạn và nộp cổng `cases` của tính năng đang làm. Ghi chú của dev (nếu có): $ARGUMENTS

1. Chạy `aisdlc status` để biết đường dẫn tính năng và trạng thái cổng.
2. Đọc `feedback.md` của tính năng nếu có và xử lý hết góp ý của dev.
3. Số case xấp xỉ số quy tắc nghiệp vụ/tiêu chí chấp nhận trong BRD, cộng một case lỗi hoặc biên cho mỗi bước Sx. Tối đa 15 case, trừ khi dev yêu cầu thêm. Không thêm luồng suy đoán.
4. `reset_sql` và `given.sql` của một case chỉ được dùng bảng đã có ở bước của case đó. Bảng do migration của bước sau tạo không được xuất hiện trong case của bước trước, cũng không được nằm trong `reset_sql`. Dọn bảng đó trong `given.sql` của case thuộc bước tạo bảng.
5. Case kiểm một quy tắc BRD thì ghi `rules: [BR-n]`.
   Case phải **cụ thể**: dùng giá trị thật (không ghi 'giá trị hợp lệ'). Không viết case để dễ pass; case phải fail trước khi code (lệnh submit sẽ chạy kiểm).
6. **Rủi ro:** nếu có `risk.yaml` của tính năng, đọc nó (chỉ đọc, không sửa) và viết đủ case theo `requirements` của mức đó:
   `idempotent` → case gửi trùng `when.repeat: 2` (hoặc hơn) cho mỗi endpoint ghi; `concurrent` → case `when.parallel` ≥ `threads` và `then.statusCounts`; `boundary` → case `kind: boundary` cho mỗi rule BRD gắn `[biên]`; `rule-pos-neg` → mỗi rule có case đúng và case sai/biên. Luồng lite: điền `impact` trong `cases.yaml` (xem `aisdlc policy show`; `level` phải thuộc thang) và, khi tính năng ghi dữ liệu tiền hoặc trạng thái (hoặc mức yêu cầu `invariant`), khai `invariants` trong `cases.yaml`: mỗi mục `{ name, sql }`, `sql` là `SELECT`/`WITH` trả về các dòng vi phạm, kiểm sau mọi case. Luồng đầy đủ khai `invariants` trong `design.yaml`, không trong `cases.yaml`.
7. Chạy `aisdlc submit cases`. Sửa theo findings rồi nộp lại cho tới khi hết findings.
8. **Dừng.** Nói với dev: "Trước khi duyệt, `aisdlc review cases` in review card (cờ đỏ, giả định, tóm tắt). Dev duyệt bằng `aisdlc approve cases` ở terminal riêng (lệnh cần console thật)."
   Nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm: "hoặc gõ `/aisdlc:approve cases` để duyệt ngay trong Claude Code (hiện hộp xác nhận), hoặc duyệt trên web"; ngược lại giữ lời nhắc ở terminal/web.
   Không tự duyệt khi dev chưa yêu cầu trong cuộc trò chuyện này, không dùng `!`.
