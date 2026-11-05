# AISDLC — Quy trình phát triển phần mềm với agent (Claude Code) có kiểm soát chất lượng

Ngày: 2026-11-02 · Trạng thái: đã duyệt (2026-11-02)

Ký hiệu: **[Quyết định của người dùng]** là điều người dùng đã chốt. **[Lựa chọn của Claude]** là đề xuất mặc định, người dùng có thể đổi.

---

## 1. Vì sao cần

Khi giao cho agent (Claude Code), tính năng thường chạy được nhưng agent **sinh ra quá nhiều**: nhiều code, nhiều file, nhiều nội dung. Dev không đọc xuể, nên **không biết agent có đang làm đúng ý mình không** cho tới khi xong. Lúc đó sửa thì đã muộn.

Mục tiêu là đưa kiểm soát chất lượng lên **trước** khi code và **trong lúc** code, thay vì dồn hết vào review cuối:

- **Trước khi code:** chốt BRD, thiết kế hệ thống (sequence), thiết kế database và bộ case input → output (unit test + eval harness). Agent chỉ được làm trong khung đã duyệt.
- **Trong lúc code:** agent đi theo từng bước của sequence. Dev thấy được agent đang ở đâu, kết quả từng bước ra sao, và có thể chỉnh hướng sớm.
- **Sau khi code:** review đối chiếu theo luồng sequence và kết quả eval, không đọc toàn bộ diff. Trọng tâm là **input và output có đúng mong muốn không**.

## 2. Người dùng và cách làm việc

- Team **4 dev**, backend **Java Spring Boot**. **[Quyết định của người dùng]**
- **Một dev làm một tính năng từ đầu đến cuối**: tự viết hoặc duyệt BRD, thiết kế, case test, giám sát agent và review cuối. **[Quyết định của người dùng]** Vì không có người duyệt chéo, các **cổng tự động** và một **agent review độc lập** đóng vai "người thứ hai".
- **Dev chạy Claude Code trong terminal của mình. Web chỉ theo dõi và duyệt**, không tự khởi chạy agent. **[Quyết định của người dùng]**
- Công cụ team đang dùng: **Jira** (task), **Confluence** (tài liệu), **GitLab** (code, MR, CI). **[Quyết định của người dùng]** AISDLC không thay thế ba công cụ này mà là lớp quy trình và chất lượng đặt lên trên.

## 3. Mục tiêu, không phải mục tiêu, phản mục tiêu

**Mục tiêu**
1. Một quy trình theo bước, có cổng duyệt, mà Claude Code tự tuân theo nhờ bộ công cụ trong repo.
2. Eval harness **tầng 1** chấm điểm từng tính năng trước khi merge. **[Quyết định của người dùng: ưu tiên trước]**
3. Web gồm bảng quy trình (trang chính), trang review từng tính năng và màn theo dõi agent đang chạy. **[Quyết định của người dùng: cả ba, trang chính là bảng quy trình]**
4. Eval **tầng 2**: theo dõi chất lượng theo thời gian và khi đổi cấu hình agent. **[Quyết định của người dùng: làm sau tầng 1]**

**Không phải mục tiêu**
- Web khởi chạy hoặc điều khiển agent từ xa.
- Thay Jira, Confluence hoặc GitLab.
- Hỗ trợ ngôn ngữ ngoài Java/Spring Boot ở giai đoạn đầu. Thiết kế nên để ngỏ khả năng mở rộng, nhưng không làm sẵn.

**Phản mục tiêu** (chạy được nhưng vẫn coi là thất bại)
- Quy trình nặng tới mức dev bỏ qua, hoặc mỗi tính năng nhỏ phải viết tài liệu dài. Tài liệu mỗi bước phải **ngắn, dạng template**, agent soạn nháp và dev chỉ sửa.
- Agent tự viết test để code của nó pass. Case input → output phải do dev duyệt **trước** khi code và bị **khóa**, agent không được sửa trong bước code.
- Điểm của agent review thay cho quyết định của dev. Điểm đó chỉ để tham khảo.
- Web trở thành nơi nhập liệu thứ tư, trùng với Jira.
- Lại sinh ra thêm một lớp nội dung dài mà dev phải đọc. Mọi báo cáo ưu tiên dạng tóm tắt: số liệu, pass/fail, điểm lệch so với thiết kế.

## 4. Tổng quan giải pháp

Gồm hai phần, làm theo thứ tự:

1. **Bộ công cụ quy trình cho Claude Code** (gói thành *plugin Claude Code* để cài cho mọi repo của team). **[Lựa chọn của Claude]** Gồm CLAUDE.md quy tắc, template tài liệu, slash command cho từng bước, hook chặn hoặc báo cáo, và eval harness tầng 1.
2. **Web AISDLC**: máy chủ nội bộ dùng chung cho 4 người, nhận sự kiện từ hook, hiển thị bảng, cho duyệt cổng và lưu điểm.

Phần 1 phải dùng được một mình, chưa cần web. Đây là giai đoạn 1.

## 5. Quy trình: các bước và cổng

Mỗi tính năng gắn với **một ticket Jira** (ví dụ `PAY-123`). Tài liệu của tính năng nằm trong repo dịch vụ, ở `aisdlc/features/<JIRA-KEY>/`, nằm cạnh code để agent đọc trực tiếp và được version bằng git. **[Lựa chọn của Claude]** Tên thư mục cụ thể do builder quyết.

Mỗi cổng có hai loại kiểm tra: **tự động** (phải pass) và **dev duyệt**. Khi dev duyệt, hệ thống ghi lại **hash nội dung** của tài liệu tại thời điểm duyệt. Nếu tài liệu bị sửa sau đó, cổng tự mở lại. **[Lựa chọn của Claude]**

| Bước | Tài liệu đầu ra (ngắn, theo template) | Kiểm tra tự động | Dev duyệt |
|---|---|---|---|
| **0. Khởi tạo** | Thư mục tính năng, lấy tiêu đề và mô tả từ Jira | Ticket tồn tại | — |
| **1. BRD** | `brd.md`: mục tiêu, trong và ngoài phạm vi, quy tắc nghiệp vụ, tiêu chí chấp nhận | Đủ các mục bắt buộc của template | Có |
| **2. Thiết kế** | `design.md`: **sequence** (Mermaid) chia thành các bước đánh số `S1…Sn`; **schema DB** (migration Flyway/Liquibase dạng nháp, theo công cụ repo đang dùng); **API contract** (OpenAPI hoặc bảng endpoint). Mỗi bước `Sx` khai báo **phạm vi file/package được phép sửa** và **ngân sách** (số file, số dòng ước tính). | Mermaid parse được, migration hợp lệ, mỗi bước có phạm vi và ngân sách | Có |
| **3. Case test / Eval** | `cases.yaml`: các case **input → output mong muốn**, gắn nhãn bước `Sx` và loại (chính / lỗi / biên), sinh ra thành test JUnit/API chạy trên ứng dụng Spring Boot với DB thật (ví dụ Testcontainers) | Test compile được và **fail đúng như kỳ vọng** (chưa có code); mỗi bước `Sx` có ít nhất một case; có case lỗi | Có, rồi **khóa** bộ case |
| **4. Agent code** | Code theo từng bước `S1…Sn`, mỗi lần một bước | Sau mỗi bước: chạy case của bước đó và các bước trước, kiểm phạm vi file, kiểm ngân sách. Hook gửi sự kiện về web | Dev có thể **tạm dừng** hoặc **gửi chỉ dẫn** giữa chừng |
| **5. Review** | Báo cáo eval tầng 1 (mục 6); diff **nhóm theo bước sequence**; điểm của agent review độc lập | Mọi cổng cứng ở mục 6 pass | Có, rồi tạo hoặc cập nhật **MR GitLab**. CI chạy lại harness |
| **6. Xong** | MR được merge, điểm lưu cho tầng 2 | CI xanh | — |

Quy tắc chung (ghi trong CLAUDE.md và được hook cưỡng chế):
- Agent **không được sửa code sản phẩm** khi cổng 2 và 3 chưa duyệt.
- Agent **không được sửa** `cases.yaml` và test sinh từ nó sau khi bộ case đã khóa. Muốn đổi thì phải quay về bước 3 và dev duyệt lại.
- Sửa file **ngoài phạm vi** đã khai báo cho bước hiện tại thì bị chặn, hoặc bị đánh dấu để dev chấp nhận. Builder chọn mức nào, nhưng tối thiểu phải hiện rõ trên báo cáo.
- Trả lại ở bất kỳ cổng nào: comment của dev trở thành chỉ dẫn cho agent ở lượt kế tiếp.

**Slash command dự kiến** **[Lựa chọn của Claude]** (tên do builder quyết): khởi tạo từ Jira key, soạn BRD, soạn thiết kế, soạn case, code một bước, chạy eval, gửi review. Mỗi lệnh chỉ chạy khi cổng trước nó đã mở.

## 6. Eval harness

### Tầng 1: chấm từng tính năng (ưu tiên, giai đoạn 1)

**Cổng cứng** (phải đạt mới qua bước 5):
1. **100% case input → output pass**.
2. Bộ case bị khóa **không bị sửa** (so hash với lúc duyệt).
3. Build pass, test có sẵn của repo pass (không phá chức năng cũ).
4. Schema DB thực tế **khớp** thiết kế đã duyệt: không có bảng hoặc cột ngoài thiết kế.
5. Không có file sửa ngoài phạm vi khai báo, trừ khi dev đã chấp nhận từng file.
6. Lint, static analysis và quét bảo mật mà repo đang dùng không có lỗi mới.

**Chỉ số mềm** (hiện trên báo cáo, cảnh báo chứ không chặn):
- Số dòng và số file thực tế so với ngân sách của từng bước. Mặc định cảnh báo khi vượt 1,5 lần. **[Lựa chọn của Claude]**
- Coverage trên các dòng mới hoặc đã sửa. Mặc định ngưỡng cảnh báo 80%. **[Lựa chọn của Claude]**
- **Agent review độc lập:** một subagent với ngữ cảnh sạch, chỉ được đưa BRD, thiết kế, bộ case và diff (không có lịch sử suy luận của agent đã code). Nó chấm theo checklist cố định: đúng sequence, xử lý lỗi, code thừa hoặc trừu tượng thừa, đặt tên, bảo mật. Kết quả là điểm kèm danh sách vấn đề ngắn, mỗi vấn đề trỏ tới bước `Sx` và dòng code.

**Báo cáo eval**: một file kết quả máy đọc được (JSON) cộng một bản tóm tắt ngắn. Web và CI dùng chung file này. Định dạng do builder quyết.

### Tầng 2: chất lượng theo thời gian (giai đoạn 3)

- Lưu điểm tầng 1 của mọi tính năng kèm **phiên bản cấu hình agent**: hash của CLAUDE.md, plugin, skill và tên model.
- Trang xu hướng trên web: tỉ lệ pass lần đầu, số lần bị trả lại mỗi cổng, độ vượt ngân sách, điểm agent review, theo thời gian và theo phiên bản cấu hình.
- **Bộ bài chuẩn**: một vài tính năng mẫu cố định (có sẵn BRD, thiết kế, case). Chạy lại mỗi khi đổi cấu hình agent để so sánh trước và sau.

## 7. Web AISDLC

**Stack** **[Lựa chọn của Claude]**: backend **Spring Boot** (team tự bảo trì được), frontend **React + TypeScript**, cập nhật trực tiếp bằng **SSE**. Chạy trên **máy chủ nội bộ** dùng chung.

**Các màn hình**
- **A. Bảng quy trình (trang chính):** mỗi tính năng là một thẻ, xếp theo cột BRD → Thiết kế → Test/Eval → Agent code → Review → Xong. Thẻ hiện Jira key, tên chủ tính năng, trạng thái cổng (chờ duyệt / bị trả lại / đang chạy) và điểm eval tóm tắt.
- **B. Trang review tính năng:** trái là các bước; giữa là tài liệu của bước đang duyệt (BRD, sequence render từ Mermaid, schema, bảng case); phải là checklist cổng, kết quả kiểm tra tự động, nút **Duyệt** / **Trả lại** và ô comment. Ở bước Review: diff nhóm theo bước `Sx`, báo cáo eval, danh sách vấn đề của agent review.
- **C. Theo dõi agent đang chạy:** danh sách bước `S1…Sn` với trạng thái, file và số dòng đã sửa mỗi bước so với ngân sách, case pass/fail theo thời gian thực, cảnh báo sửa ngoài phạm vi. Nút **Tạm dừng** và **Gửi chỉ dẫn**.
- **D. Chất lượng (tầng 2, giai đoạn 3):** trang xu hướng ở mục 6.

**Luồng dữ liệu** **[Lựa chọn của Claude]**
- Hook của Claude Code trên máy dev gửi sự kiện (bước bắt đầu/kết thúc, file sửa, kết quả test, kết quả eval) và **bản chụp tài liệu kèm hash** khi dev gửi duyệt lên web qua HTTP.
- Web lưu sự kiện, bản chụp, lịch sử duyệt và điểm trong DB riêng.
- **Trạng thái cổng**: từ giai đoạn 2, web là nơi lưu chính. Hook và lệnh trên máy dev hỏi web xem cổng đã mở chưa. Giai đoạn 1 (chưa có web) dùng file trạng thái cục bộ trong thư mục tính năng, dev duyệt bằng lệnh trong terminal. Bộ công cụ nên đọc cổng qua một lớp chuyển đổi được giữa hai nguồn.
- **Tạm dừng / chỉ dẫn**: dev bấm trên web, hook ở máy dev đọc lại ở lần gọi tool kế tiếp: chặn khi đang tạm dừng, hoặc chèn chỉ dẫn vào ngữ cảnh của agent. Builder cần kiểm tra cơ chế hook của phiên bản Claude Code đang dùng để chọn loại hook phù hợp.
- **Jira:** thẻ gắn với ticket; web lấy tiêu đề và mô tả; khi qua cổng thì chuyển trạng thái ticket theo bảng ánh xạ cấu hình được.
- **GitLab:** bước Review tạo hoặc cập nhật MR, gắn link báo cáo eval; CI chạy lại harness tầng 1.
- **Confluence (giai đoạn 3, tùy chọn):** đẩy BRD và thiết kế đã duyệt lên Confluence cho người ngoài team đọc.

## 7b. Các lựa chọn bổ sung sau vòng review thiết kế

- **Mức chống gian lận của cổng** **[Lựa chọn của Claude]**: giả định agent mắc lỗi thật thà, không cố ý qua mặt. Tuy vậy, hook phải chặn agent tự duyệt: chặn lệnh duyệt và chặn ghi vào file trạng thái cổng và bộ case đã khóa qua *mọi* công cụ, kể cả Bash, không chỉ Edit/Write. Từ giai đoạn 2, bản ghi duyệt lưu trên web, ngoài repo. CI hỏi web để xác nhận hash đã duyệt thay vì tin file trong repo. Ở giai đoạn 1, CI chỉ cảnh báo khi hash lệch.
- **Web không truy cập được (từ giai đoạn 2)** **[Lựa chọn của Claude]**: hook *đóng*, tức là không mở cổng mới và chặn sửa code nếu chưa từng mở cổng. Cổng đã mở trước đó (có lưu cache cục bộ kèm hash) vẫn dùng được. Sự kiện được xếp hàng và gửi lại khi web hoạt động trở lại.
- **Agent review trong CI** **[Lựa chọn của Claude]**: CI chỉ chạy các cổng *tất định* ở mục 6 (case, hash, build, schema, phạm vi, lint). Agent review chạy trên máy dev, kết quả đính kèm báo cáo eval. CI không phụ thuộc vào việc runner gọi được Claude.
- **Jira / GitLab tự host hay cloud, token theo người hay tài khoản dịch vụ**: xác minh khi bắt đầu giai đoạn 2. Mặc định thao tác ghi đứng tên dev thực hiện (token cá nhân). **[Lựa chọn của Claude]**

## 7c. Chốt sau vòng hỏi cuối **[Quyết định của người dùng: đồng ý các mặc định đề xuất]**

1. **Phạm vi bộ case:** test qua HTTP API và kiểm trạng thái DB. Service nội bộ khác giả lập bằng WireMock. Kafka/MQ (nếu có) kiểm message đã gửi, chạy bằng container. Job theo lịch **để sau** giai đoạn 1.
2. **Bước code:** agent **dừng sau mỗi bước `Sx`**, báo kết quả (case pass/fail, file và dòng so với ngân sách, sửa ngoài phạm vi). Dev cho phép thì mới sang bước kế. Nút tạm dừng / chỉ dẫn trên web vẫn có để can thiệp giữa một bước.
3. **Luồng nhẹ:** sửa bug và thay đổi nhỏ bỏ qua BRD và thiết kế, nhưng **bắt buộc** có bộ case được duyệt và khóa trước khi code. Khi khởi tạo, dev chọn loại luồng (đầy đủ / nhẹ). Phạm vi file vẫn phải khai báo, khai báo ngắn trong file case.
4. **Toolchain (giả định, phải xác minh trên repo thí điểm trước khi code):** Maven, Flyway, Docker chạy được trên máy dev và GitLab runner (dùng cho Testcontainers), máy dev chạy Linux. Phiên bản Java/Spring Boot, lint và quét bảo mật lấy theo repo thí điểm. Nếu giả định sai thì điều chỉnh phần sinh test và CI.

## 8. Ràng buộc

- Hệ thống chạy trong **mạng nội bộ**; mã nguồn và tài liệu không gửi ra dịch vụ ngoài, ngoài việc Claude Code đang được team sử dụng.
- Web phải chạy được khi Jira, GitLab hoặc Confluence tạm không truy cập được (chỉ mất phần đồng bộ, không mất chức năng duyệt).
- Bộ công cụ không được làm chậm đáng kể vòng làm việc của dev. Kiểm tra sau mỗi bước chỉ chạy case liên quan; bộ đầy đủ chạy ở bước Review và CI.

## 9. Thứ tự triển khai

1. **Giai đoạn 1: bộ công cụ + eval tầng 1** (chưa có web): template, CLAUDE.md, slash command, hook chặn (cổng, khóa case, phạm vi file), harness sinh test từ `cases.yaml`, báo cáo eval, agent review, duyệt bằng lệnh terminal. Thử trên một repo Spring Boot thật của team với 1–2 tính năng.
2. **Giai đoạn 2: web A, B, C** + nhận sự kiện từ hook + duyệt trên web + tạm dừng / chỉ dẫn + tích hợp Jira và GitLab MR.
   - 2a (web chỉ xem): **đã xong** trên nhánh `aisdlc-web-2a` @ `b9e4798` (2026-11-03), **chưa merge vào `main`**.
   - Review nhanh (§12, `2026-11-03-aisdlc-review-design.md`): R1 (CLI) và R2 (trang B) làm ngay sau 2a; R3 làm cùng 2b.
   - 2b (duyệt trên web, tạm dừng, chỉ dẫn), rồi 2c (Jira, GitLab, chỉ số).
3. **Giai đoạn 3: tầng 2** (lưu điểm theo phiên bản cấu hình, trang xu hướng, bộ bài chuẩn) + Confluence.

## 10. Để builder quyết

- Tên thư mục, tên lệnh, định dạng chi tiết của `cases.yaml` và báo cáo eval.
- Cách sinh test từ `cases.yaml` (code generation hay test JUnit tham số hóa đọc YAML khi chạy).
- Cách đo "schema khớp thiết kế" và "đúng sequence" cụ thể.
- Schema DB của web, cách đăng nhập (ưu tiên dùng tài khoản công ty nếu có), cách triển khai máy chủ.
- Mức chặn hay cảnh báo cho sửa ngoài phạm vi (tối thiểu phải hiện rõ).

## 11. Bổ sung sau khi tham khảo Warp Factories (2026-11-02) **[Lựa chọn của Claude, chờ người dùng xác nhận]**

Nguồn tham khảo: docs.warp.dev/factories (tổng quan, How Factories work, Scorers, Benchmarks, Inbox, Factory MCP).

**Giữ nguyên, không lấy từ Warp:**
- Agent chạy trên cloud tự động từ đầu đến cuối, nhận việc từ Slack, có "foreman" điều phối. Hướng này trái với quyết định "dev chạy terminal, web chỉ theo dõi và duyệt", và với ràng buộc chạy trong mạng nội bộ.
- Factory MCP (giao việc qua lại giữa máy local và cloud): không có cloud agent nên không cần.

**Lấy vào giai đoạn 1:**
1. **Bước phân loại (triage) khi khởi tạo:** agent đọc ticket, đề xuất luồng đầy đủ hay luồng nhẹ kèm lý do một dòng, và liệt kê điểm chưa rõ. Dev chọn luồng. Tương ứng với việc foreman của Warp bỏ qua bước cho thay đổi nhỏ, nhưng ở đây người chọn là dev.
2. **Câu hỏi mở chặn cổng:** BRD và thiết kế có mục `Câu hỏi mở`. Còn câu chưa trả lời (dòng `- [ ]`) thì cổng không qua. Tương ứng với "Asked question" của Warp: agent hỏi chứ không tự đoán.
3. **Agent review chấm theo nhãn thay vì thang 0–10:** mỗi tiêu chí trong checklist nhận một nhãn `ĐẠT / CẦN_SỬA / NGHIÊM_TRỌNG` kèm lý do. Nhãn ổn định hơn điểm số tự do. Warp dùng cách này cho Scorer (classification + reasoning + ngưỡng pass). Điểm tổng được tính từ các nhãn.
4. **Chặn merge bằng GitLab chứ không chỉ bằng hook:** Warp nhấn mạnh việc merge do quyền của repo cưỡng chế. README hướng dẫn bật protected branch và bắt buộc job `aisdlc-eval` pass mới được merge MR.

**Lấy vào giai đoạn 2 (web):**
5. **Hộp việc của tôi:** trên bảng quy trình (A) có thêm bộ lọc hoặc khung "việc chờ tôi". Gồm cổng chờ duyệt, bước chờ `step accept`, câu hỏi mở, step finish bị fail, eval không đạt. Mục tự biến mất khi đã xử lý. Bảng A vẫn là trang chính.
6. **Chỉ số theo dõi:** thời gian mỗi bước (cycle time), số lần bị trả lại mỗi cổng, và chi phí token mỗi tính năng (nếu lấy được từ Claude Code).

**Lấy vào giai đoạn 3 (tầng 2):**
7. **Bộ bài chuẩn theo kiểu Benchmark:** mỗi bài có đầu vào cố định (BRD, thiết kế, case đã duyệt), chạy **nhiều lần** cho mỗi cấu hình vì agent không tất định. Ghi tỉ lệ đạt, thời gian, chi phí. Đầu vào của mỗi lần chạy được lưu lại để sửa bộ bài sau này không làm sai lệch kết quả cũ. Mỗi lần chỉ đổi một thứ trong cấu hình.
8. **Đề xuất tự cải thiện:** gom các lỗi lặp lại (cùng loại issue của agent review, cùng loại cổng hay bị trả lại) thành đề xuất sửa CLAUDE.md, command hoặc checklist, gửi dưới dạng **MR để dev review**, không tự áp dụng.

## 12. Bổ sung sau khi tham khảo Spec-Driven Development và formal verification (2026-11-03) **[Lựa chọn của Claude, chờ người dùng xác nhận]**

Nguồn tham khảo: hai bài của Jaroslaw Wasowski trên levelup.gitconnected.com, "Stop Writing Specs. Start Writing Facts." (05/2026) và "Formal Verification in Spec-Driven Development — Enterprise Level" (06/2026).

**AISDLC đã đúng hướng ở các điểm sau, giữ nguyên:**
- Checker có tiếng nói cuối cùng: cổng cứng do máy chạy, agent review chỉ là chỉ số mềm.
- `cases.yaml` là "facts": chạy được, khóa bằng hash, phải fail trước khi có code. BRD và `design.md` là tài liệu cho người đọc (dev duyệt, audit), đúng vùng mà spec dạng văn xuôi vẫn có giá trị.
- Chống lệch giữa mô hình và code: `CASES_LOCKED`, `SCHEMA_MATCH`, `SCOPE`, cổng STALE.

**Lấy vào ngay sau 2a:**
1. **Review nhanh trước khi agent thực thi:** mục Giả định trong template, review card, cờ đỏ tự động, hình sinh tất định trên trang B (bản đồ bước → vùng code, ER diff, ma trận quy tắc × case). Chi tiết ở `2026-11-03-aisdlc-review-design.md`.

**Để sau (chưa có spec):**
2. **Bất biến DB:** `design.yaml` có mục `invariants`, mỗi mục là một câu SQL phải trả 0 dòng, chạy sau **mọi** case. Vi phạm thì fail cứng.
3. **Case property-based:** `kind: property` với bộ sinh dữ liệu, số mẫu và seed cố định (để `CASES_LOCKED` vẫn có nghĩa). Fail thì in phản ví dụ kèm seed. Gợi ý case biên N−1, N, N+1 từ các con số trong quy tắc BRD.
4. **Mức chặt theo rủi ro (blast radius):** bước `risk: high` (tiền, hạn mức, số dư) phải có case property, bất biến, case `concurrent` (N request song song rồi kiểm bất biến), và chỉ số mềm mutation testing trên dòng thay đổi. Bước `low` giữ nguyên.
5. **Stub khớp hợp đồng:** khi có OpenAPI của hệ thống downstream, kiểm request và response của stub WireMock theo schema đó.

**Không làm:** TLA+/Alloy cho dịch vụ CRUD. Chỉ cân nhắc khi có luồng phân tán thật (ví dụ saga chuyển tiền liên hệ thống), dùng như artifact tùy chọn chạy qua `checks` trong `config.yaml`.
