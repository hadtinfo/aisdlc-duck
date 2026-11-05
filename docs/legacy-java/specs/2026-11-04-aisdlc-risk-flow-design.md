# AISDLC — Lint sau mỗi bước, trần số vòng sửa, quy trình theo mức rủi ro

Ngày: 2026-11-04. Làm sau nhánh `risk-policy` (spec `2026-11-04-aisdlc-risk-policy-design.md`) và dùng lại chính sách rủi
ro của nhánh đó.

Nhãn: **[Người dùng]** = người dùng đã quyết. **[Lựa chọn của Claude]** = đề xuất, builder đổi được nếu có lý do.

## 1. Vì sao cần

Người dùng đọc bài viết về Minions của Stripe (agent chạy một mạch tới PR; lint nhanh tất định; tối đa 2 vòng CI; review PR
cuối) và chốt ba bổ sung cho AISDLC **[Người dùng]**:

1. Lint nhanh tất định sau mỗi bước.
2. Trần số vòng sửa.
3. Nới quy trình theo mức rủi ro: story mức thấp đi gần như one-shot, mức cao giữ đủ các cổng.

Với story mức thấp, người dùng chọn **phương án (a)** **[Người dùng]**:
- agent vẫn viết đủ BRD, thiết kế và case;
- các cổng tự duyệt khi sạch;
- dev chỉ xem ở cổng Review cuối.

Không chọn bỏ hẳn BRD và thiết kế.

## 2. Mục tiêu, không làm, phản mục tiêu

**Mục tiêu**
- Mỗi bước có lint tất định do dự án tự khai. Lint đỏ thì bước chưa xong và agent nhận lỗi để sửa.
- Mỗi bước và mỗi cổng có bộ đếm số lần agent nộp bị chặn. Vượt trần thì agent bị tạm dừng cho tới khi dev xem.
- Chính sách rủi ro khai `flow` theo mức, cho biết cổng nào dev duyệt tay và cổng nào tự duyệt.

**Không làm**
- Không chọn test theo file đã đổi. Có thể làm sau.
- Không chạy agent trong container hay devbox.
- Không gắn rule theo thư mục.

**Phản mục tiêu** (có những điều này thì coi như hỏng, dù code chạy)
- Có cách nào đó để bỏ được việc dev duyệt cổng **Review**.
- Story bị nâng mức giữa chừng mà các cổng đã tự duyệt vẫn được giữ như dev đã duyệt.
- Agent tự duyệt được cổng, hoặc tự gỡ được tạm dừng.
- Hook gọi mạng.
- Lõi gắn cứng một công cụ lint.

## 3. Lint sau mỗi bước

Cấu hình trong `aisdlc/config.yaml`. Dự án tự chọn công cụ:
```yaml
lint:
  check: "mvn -q spotless:check checkstyle:check"   # bắt buộc nếu bật lint
  fix: "mvn -q spotless:apply"                       # tùy chọn
  timeoutSeconds: 60
```
- Không có khối `lint` thì không lint, hành vi giữ như cũ.

**Cách chạy:**
- `aisdlc step finish Sx` chạy thêm lint, **trước** khi chạy case:
  1. chạy `fix` (nếu có);
  2. chạy `check`.
- Exit ≠ 0 hoặc quá `timeoutSeconds` thì `step finish` thất bại. Agent nhận tối đa 40 dòng cuối của output.
- File mà `fix` sửa phải nằm trong phạm vi của bước. Nếu `fix` sửa ra ngoài phạm vi thì CLI hoàn tác các file đó và báo lỗi.
  **[Lựa chọn của Claude]**

**Ghi nhận và hiển thị:**
- Kết quả ghi vào `steps.json` của bước: `lint: {passed, at, tail}`.
- Kết quả hiện trên thẻ bước, ở CLI và trên web.
- `step accept` từ chối khi lint của lần `finish` gần nhất chưa qua.

**Eval:** thêm kiểm tra cứng `LINT`, chạy `check` trên toàn bộ thay đổi. Kiểm tra này chỉ có khi cấu hình có khối `lint`.

## 4. Trần số vòng sửa

**Một vòng** là một lần lệnh của agent bị chặn:
- `step finish` thất bại (lint, case, phạm vi hoặc ngân sách);
- `submit <gate>` bị validator chặn;
- `aisdlc eval` FAIL.

**Đếm:**
- Bộ đếm theo từng bước và từng cổng, lưu trong file trạng thái `aisdlc/features/<KEY>/rounds.json`. Hook chặn agent ghi file
  này, giống `steps.json`.
- Lần nộp thành công thì bộ đếm của đơn vị đó về 0.

**Trần:**
- Đặt trong chính sách rủi ro theo mức: `maxRounds: { low: 2, medium: 3, high: 3 }`.
- Mặc định là 3 khi chính sách không khai, hoặc tính năng chưa có `risk.yaml`.
- `risk.yaml` chụp lại giá trị trần lúc phân loại, giống cách chụp `requirements`.

**Vượt trần:**
- CLI ghi trạng thái tạm dừng local vào `.aisdlc/pause/<KEY>.json`, ghi rõ lý do và đơn vị nào vượt trần.
- Hook đọc file này và chặn mọi tool ghi với mã `PAUSED`, dùng chung thông điệp và luồng của 2b-2. Lý do hiển thị: "Đã thử N lần
  chưa qua ở <Sx|cổng>, cần dev xem".
- Có web thì gửi sự kiện `ROUNDS_EXCEEDED`. Web hiện trạng thái dừng và lý do.

**Gỡ dừng:**
- Dev chạy `aisdlc resume` (cần console thật và gõ lại key), hoặc bấm "Tiếp tục" trên web giống 2b-2.
- Gỡ dừng thì bộ đếm của đơn vị đó về 0.
- Agent không có lệnh nào để gỡ dừng.

## 5. Quy trình theo mức rủi ro (phương án a)

Chính sách rủi ro thêm khối `flow` theo mức:
```yaml
flow:
  low:    { brd: auto, design: auto, cases: auto, step: auto }
  medium: { brd: manual, design: manual, cases: manual, step: auto }
  high:   { brd: manual, design: manual, cases: manual, step: manual }
```
- **Giá trị:** `auto` | `manual`.
- **Mặc định:** thiếu khối hoặc thiếu key thì là `manual`.
- **Review:** không có key `review`; Review luôn `manual` (phản mục tiêu 1). Validate chính sách báo lỗi nếu có key `review`.
- **Mẫu `generic` và `banking`:** thêm khối như trên.
- **Giao diện web:** tab "Yêu cầu theo mức" thêm hàng `flow` và `maxRounds`.

### 5.1 Tự duyệt khi nào

Một cổng hoặc bước `auto` được tự duyệt khi đủ cả ba điều kiện:
- lệnh nộp thành công (validator không chặn; với bước là `finish` qua);
- review card **không có cờ đỏ mức HIGH**;
- mức rủi ro áp cho cổng đó cho phép `auto`.

**Mức áp cho từng cổng**, vì cổng BRD chưa có đánh giá đầy đủ:
- **BRD:** mức tạm (`riskPreview`, chỉ dựa vào từ khóa), nâng lên mức `default` của chính sách nếu thấp hơn.
- **Thiết kế trở đi:** mức trong `risk.yaml`.

**[Lựa chọn của Claude]**

**Ai ghi bản duyệt:**
- **Không có web:** CLI ghi approval với `approver: "auto"` và `rule: "flow:<level>"`.
- **Có web:** CLI không có quyền duyệt, vì token CLI không duyệt được gì. **Server** tự duyệt khi nhận submission sạch:
  - server kiểm cờ trong review card của submission;
  - server tính mức từ chính sách đang có hiệu lực, dùng chính code core;
  - bản duyệt ghi `by: auto`.
- **Hiển thị:** CLI, web và review card ghi rõ "tự duyệt (mức thấp)" cho mọi bản duyệt auto.

**CI:** chấp nhận approval `auto` chỉ khi `risk.yaml` đã khóa ở cổng Case xác nhận rằng mức đó có `flow` cho cổng tương ứng
là `auto`. `risk.yaml` chụp thêm `flow` của mức đó.

### 5.2 Nâng mức giữa chừng

Nếu mức mới (`submit design`, `submit cases`, `risk set`, `reclassify`) làm một cổng **đã tự duyệt** chuyển sang `manual`:
- bản duyệt auto của cổng đó bị đánh dấu `superseded`;
- cổng về trạng thái chờ dev duyệt tay. Nội dung không đổi nên không cần agent nộp lại;
- agent không được đi tiếp: hook coi như cổng chưa duyệt.

CLI in rõ: "Mức tăng low → high: cổng BRD, Thiết kế cần dev duyệt lại."

Hạ mức thì không biến bản duyệt tay thành auto. Bản duyệt tay luôn giữ nguyên.

### 5.3 Bước tự accept

Khi `step: auto`, `step finish` thành công thì CLI tự accept bước. Điều kiện:
- lint qua;
- case qua;
- phạm vi và ngân sách sạch;
- không có file nào được cho phép thêm (ALLOW_SCOPE) trong bước.

Nếu bước có ALLOW_SCOPE thì vẫn cần dev accept. **[Lựa chọn của Claude]**

## 5.4 Chi tiết đã chốt **[Lựa chọn của Claude]**

- **Chế độ web, nâng mức:** server tự làm, CLI không phải gọi thêm gì.
  - Khi nhận sự kiện `RISK_CLASSIFIED` hoặc `RISK_OVERRIDDEN`, server tính lại `flow` của từng cổng theo mức mới, dùng code
    core và chính sách có hiệu lực.
  - Server đánh dấu `superseded` các approval auto mà nay là `manual`. Cổng đó về trạng thái chờ duyệt trên server.
  - CLI và hook thấy trạng thái này qua cache cổng có chữ ký sẵn có.
  - Token CLI vẫn không sửa được approval nào.
- **Chế độ local, tự duyệt:** giữ mô hình an toàn hiện tại: giả định agent có thể nhầm lẫn, không giả định agent cố ý tấn công.
  - Bản duyệt auto chỉ sinh ra như hệ quả của `submit` hoặc `step finish`, khi `flow` của mức hiện tại cho phép. Không có lệnh
    nào để agent "duyệt auto" trực tiếp.
  - Agent không sửa được chính sách, `risk.yaml`, `gates.json` hay `rounds.json`, vì hook chặn.
  - Agent không hạ được mức, vì `risk set` cần console thật.
- **Bước tự accept:** danh sách điều kiện ở §5.3 là đầy đủ. Bước không có review card riêng.
- **Mức tạm ở BRD:**
  - Dự án đặt `default: medium` thì cổng BRD sẽ không bao giờ tự duyệt. Đây là chủ ý: dự án chọn như vậy.
  - CI kiểm approval auto của BRD theo mức cuối trong `risk.yaml`. Nếu mức cuối là `manual` cho BRD thì §5.2 đã buộc dev duyệt
    lại tay trước đó, nên CI không gặp approval auto nào không hợp lệ.
- **Đếm vòng:** CLI làm hết việc đếm và tạm dừng. Validator chạy trong CLI ở cả hai chế độ. Server chỉ nhận
  `ROUNDS_EXCEEDED` để hiển thị.
- **Eval FAIL** tính vào bộ đếm của cổng **Review**. Eval ĐẠT thì bộ đếm này về 0.
- **Hoàn tác `fix` ngoài phạm vi:** repo dịch vụ luôn là git, eval đã dùng git.
  1. Chụp `git status --porcelain` cùng hash của các file đang thay đổi, trước khi chạy `fix`.
  2. Sau `fix`, file nào mới bị đổi mà nằm ngoài phạm vi bước thì `git checkout --`. File mới chưa track thì xóa.
- **File tạm dừng:**
  - 2b-2 không có file tạm dừng local; trạng thái dừng ở đó đến từ cache `FeatureControl` có chữ ký.
  - Thêm file mới `.aisdlc/pause/<KEY>.json` với `{reason: "ROUNDS", unit, count, at}`.
  - Hook kiểm cả hai nguồn. Thông điệp nêu rõ nguồn: "Dev đã tạm dừng tại web" hoặc "Đã thử N lần chưa qua ở …".
  - `aisdlc resume` chỉ gỡ được tạm dừng local. Tạm dừng do web vẫn gỡ trên web như cũ.

## 6. Kiểm thử

- **Core:**
  - parse `flow`, `maxRounds`, và lỗi khi có key `review`;
  - quyết định auto/manual theo mức, cờ và mức tạm ở BRD;
  - bản duyệt `superseded` khi nâng mức;
  - bộ đếm vòng.
- **CLI:**
  - lint chạy trong `step finish` (đỏ chặn, `fix` sửa ngoài phạm vi thì hoàn tác);
  - vượt trần thì tạm dừng, hook chặn ghi với mã `PAUSED`;
  - `aisdlc resume` cần console;
  - tự duyệt ở chế độ local;
  - nâng mức đưa cổng về trạng thái chờ duyệt;
  - `step accept` từ chối khi lint chưa qua.
- **Web:**
  - server tự duyệt submission sạch khi flow là `auto`;
  - submission có cờ HIGH thì không tự duyệt;
  - sự kiện `ROUNDS_EXCEEDED` hiển thị trạng thái dừng;
  - gỡ dừng trên web.
- **CI:** approval `auto` được chấp nhận khi `risk.yaml` xác nhận flow, và bị từ chối khi không.
- **E2E:** một tính năng mức thấp chạy từ BRD tới Review mà dev chỉ duyệt Review. Một tính năng bị nâng mức ở thiết kế thì
  cổng BRD về chờ duyệt. Lint đỏ chặn `step finish`.

## 7. Để builder quyết

- Định dạng `rounds.json` và file tạm dừng local.
- Câu chữ thông điệp.
- Hàm chọn 40 dòng output lint.
- Cách web hiển thị "tự duyệt".
