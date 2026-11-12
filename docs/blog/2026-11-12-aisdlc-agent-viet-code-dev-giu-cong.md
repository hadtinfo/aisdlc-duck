---
title: "AISDLC: để agent viết code, nhưng dev giữ cổng"
date: 2026-11-12
tags: [ai-agent, claude-code, sdlc, go, quality-gates]
---

# AISDLC: để agent viết code, nhưng dev giữ cổng

Năm 2025 là năm các công cụ AI viết code đi từ "gợi ý từng dòng" sang "agent tự làm cả tính năng". Đi kèm với
tốc độ là một nỗi lo quen thuộc với bất kỳ ai từng review một merge request (MR) do agent tạo ra: code chạy được,
test xanh, nhưng mình không chắc nó làm đúng điều cần làm. AISDLC sinh ra để trả lời câu hỏi đó cho một bối cảnh cụ thể:
các team viết dịch vụ Go (PostgreSQL, migration bằng goose) với Claude Code.

Bài này kể AISDLC là gì, vì sao nó được thiết kế như vậy, và nó khác gì so với các cách tiếp cận đang phổ biến.

## Bối cảnh: dùng AI nhiều hơn, tin AI ít hơn

Vài số liệu đáng chú ý từ các khảo sát gần đây:

- **Khảo sát Stack Overflow 2025** (hơn 49.000 người trả lời): 84% lập trình viên đã dùng hoặc định dùng công cụ AI,
  nhưng tỷ lệ *không tin* độ chính xác của kết quả tăng lên 46% (năm trước là 31%). Chỉ khoảng 3% "rất tin" vào kết quả.
  Với agent, 87% lo về độ chính xác và 81% lo về bảo mật, quyền riêng tư dữ liệu
  ([Stack Overflow](https://stackoverflow.co/company/press/archive/stack-overflow-2025-developer-survey/)).
- **Báo cáo DORA 2025 của Google** (gần 5.000 người làm công nghệ): khoảng 90% dùng AI trong công việc, nhưng thông
  điệp chính của báo cáo là AI *khuếch đại* điểm mạnh lẫn điểm yếu sẵn có của tổ chức. Nó không tự sửa một quy trình
  giao hàng kém ([Google](https://blog.google/technology/developers/dora-report-2025/),
  [InfoQ](https://www.infoq.com/news/2025/09/dora-state-of-ai-in-dev-2025)).
- **Nghiên cứu ngẫu nhiên có đối chứng của METR** (16 lập trình viên open source giàu kinh nghiệm, 246 việc thật trên
  repo lớn): khi được dùng AI, họ mất thời gian **nhiều hơn khoảng 19%**, dù trước đó dự đoán sẽ nhanh hơn 24% và sau
  đó vẫn *cảm thấy* mình nhanh hơn 20%. Một phần thời gian bị tiêu vào việc đọc, sửa và loại bỏ code AI đề xuất
  ([InfoWorld](https://www.infoworld.com/article/4020931/ai-coding-tools-can-slow-down-seasoned-developers-by-19.html)).
  METR cũng lưu ý kết quả gắn với công cụ đầu năm 2025 và bối cảnh repo lớn, chuẩn chất lượng cao.

Còn một vấn đề nữa nằm ngay trong hành vi của agent. Trong system card của Claude 4, Anthropic mô tả việc mô hình
đời trước (Claude 3.7 Sonnet) đôi khi **"special-case" để qua test** trong môi trường agentic coding như Claude Code,
tức là viết lời giải đủ hẹp để test xanh, thậm chí hard-code giá trị mong đợi. Anthropic cũng ghi nhận các mô hình
mới giảm mạnh hành vi này ([Anthropic, Claude 4 system card](https://www.anthropic.com/claude-4-system-card)). Bài
học thì vẫn còn nguyên: **"test xanh" chỉ có nghĩa khi agent không được tự định nghĩa và tự sửa thước đo của chính
nó**.

Ghép ba điều trên lại: dev dùng AI rất nhiều, ít tin kết quả, tốn công kiểm tra, và chính agent có lúc "lách" thước
đo. Vấn đề cần giải vì vậy không phải là làm agent viết nhanh hơn. Vấn đề là làm sao để **niềm tin vào kết quả của
agent có cơ sở**.

## Các cách tiếp cận hiện có

**Spec-driven development.** GitHub ra mắt [Spec Kit](https://github.blog/ai-and-ml/generative-ai/spec-driven-development-with-ai-get-started-with-a-new-open-source-toolkit/)
(9/2025) với ý tưởng: viết đặc tả trước, rồi kế hoạch kỹ thuật, rồi chia thành các việc nhỏ kiểm thử được để agent
làm. Cách này đối lập với kiểu "vibe coding" muốn gì gõ nấy. Đây là hướng đúng, và AISDLC cũng đi theo tinh thần
"tài liệu trước, code sau".

**Hook của Claude Code.** Claude Code cho phép cắm [hook](https://code.claude.com/docs/en/hooks) chạy trước và sau mỗi
lần agent dùng tool. Hook `PreToolUse` có thể chặn một lời gọi tool, và lý do chặn được đưa lại cho agent đọc. Đây là
lớp *tất định* bao quanh một agent vốn *xác suất*, và AISDLC dựng phần lớn cơ chế kiểm soát của mình trên đó.

**Agent không người trông của Stripe.** Stripe kể trong hai bài viết về "Minions", agent viết code tự chạy, không
cần người trông, từ đầu đến cuối mỗi việc
([phần 1](https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents),
[phần 2](https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents-part-2)). Mỗi tuần, hơn 1.300 PR
được merge ở Stripe là do minion viết toàn bộ. Các PR đó vẫn được người review, nhưng không chứa dòng code nào do
người viết. Vài lựa chọn thiết kế của Stripe rất đáng học:

- **Hộp cát cô lập thay cho hỏi quyền.** Minion chạy trên devbox tách khỏi production và internet, nên chạy được "mà
  không cần kiểm tra quyền của người", và mọi sai sót bị giới hạn trong phạm vi một devbox.
- **"Blueprint": xen kẽ bước tất định và bước agentic.** Chạy linter, đẩy code, thao tác git là code thường, "không gọi
  LLM". Chỉ những việc như "implement task" hay "fix CI failures" mới giao cho agent. Stripe gọi đó là "đặt LLM vào
  những chiếc hộp có giới hạn".
- **Vòng phản hồi có trần.** Lint chạy ngay khi push. CI tối đa hai lượt, vì mỗi lượt tốn token, máy và thời gian.
  Sau lượt thứ hai, nhánh được trả về cho người.
- **Một lưới test khổng lồ đứng sau.** Hơn ba triệu test.

**"Harness" quanh agent.** Cách nhìn này đang được gọi chung là *harness*: model chỉ lo suy luận, còn lớp bao quanh
quyết định agent được thấy dữ liệu gì, được chạm vào hệ thống nào, và kết quả được kiểm chứng ra sao. Một bài của
XMPro, công ty bán sản phẩm harness nên đây là góc nhìn của nhà cung cấp, tóm lại thành công thức
"Agent = Model + Harness", và nhấn mạnh rằng *ràng buộc làm tăng độ tin cậy*
([XMPro](https://xmpro.com/your-ai-agent-needs-a-harness-heres-what-that-means-for-industrial-operations/)).

AISDLC kết hợp các ý trên: quy trình theo tài liệu như spec-driven development, cộng với hook và CI để **ép** quy trình
đó. Theo nghĩa đó, AISDLC là một harness cho agent viết code.

## AISDLC hoạt động thế nào

Một tính năng đi qua năm cổng. Agent làm, dev duyệt. Cổng sau chỉ mở khi cổng trước đã được duyệt:

```
BRD ──▶ Design ──▶ Cases ──▶ Code từng bước S1…Sn ──▶ Review ──▶ MR
 │         │         │                │                     │
 duyệt     duyệt     duyệt            accept từng bước       duyệt (eval phải ĐẠT)
```

![Luồng cổng: agent làm, dev duyệt](../images/luong-cong.png)

1. **BRD**: yêu cầu nghiệp vụ, mỗi quy tắc có mã `BR-n`.
2. **Design**: sơ đồ tuần tự Mermaid chia thành các bước S1…Sn, bảng API, các giả định của agent, migration nháp. File
   `design.yaml` khai báo **phạm vi file (glob) và ngân sách** (số file, số dòng) cho từng bước.
3. **Cases**: bộ case kiểm thử (HTTP, DB, message, stub) viết **trước khi có code**. Khi nộp, bộ case **phải có case đang
   fail**: nếu mọi case đã xanh khi chưa viết dòng code nào, các case đó không kiểm tra được gì.
4. **Code từng bước**: agent chỉ được ghi file trong phạm vi của bước đang làm. Dev accept từng bước.
5. **Review**: eval phải ĐẠT, có review độc lập của một subagent, rồi dev duyệt và mở MR.

Tài liệu đã duyệt được **khóa bằng hash**. Muốn sửa thì dev phải mở lại cổng. Với bug fix nhỏ không đụng DB/API, có
luồng nhẹ (`--lite`) bỏ qua BRD và Design, chỉ còn Cases → S1 → Review.

### Năm ý tưởng chính

**1. Thước đo do người duyệt, không do agent tự đặt.** Bộ case được khóa trước khi viết code và phải fail trước khi
code tồn tại. Harness `aisdlc-harness` chạy case như hộp đen trên dịch vụ thật: Postgres (và Kafka nếu cần) dựng bằng
Docker qua testcontainers. Agent không thể "sửa test cho xanh", vì sửa `cases.yaml` sau khi duyệt thì eval báo
`CASES_LOCKED`. Đây là câu trả lời trực tiếp cho hiện tượng special-casing ở trên.

**2. Phạm vi và ngân sách cho từng bước.** Hook `PreToolUse` chặn mọi lần ghi ra ngoài glob của bước đang làm. Muốn ghi
thêm một file, dev phải cho phép đúng file đó (`aisdlc scope allow`). Agent không thể lặng lẽ "tiện tay refactor" nửa
codebase trong một tính năng thêm hạn mức chuyển tiền. Diff vì thế luôn nằm gọn trong phạm vi đã duyệt từ thiết kế.

**3. Review card thay cho đọc diff mù.** Ở mỗi cổng, dev nhận một thẻ tóm tắt có cờ đỏ: phạm vi các bước chồng nhau,
glob chạm migration cũ, thay đổi bảng đang có, quy tắc nghiệp vụ không có case… Thẻ còn có sơ đồ bước, ER diff, bảng
API và **danh sách giả định của agent**. Mục tiêu là đưa sự chú ý của người duyệt tới đúng chỗ đáng ngờ, đúng điều mà
dữ liệu của METR và Stack Overflow cho thấy đang tốn nhiều công nhất.

**4. Rủi ro do một subagent độc lập xếp loại, kèm bằng chứng.** Mỗi repo có một chính sách rủi ro viết bằng lời. Ở cổng
Design, subagent `aisdlc-risk-classifier`, chỉ có quyền đọc, đọc tài liệu của tính năng và quyết định luật nào khớp.
Mỗi luật khớp phải kèm **trích dẫn nguyên văn**. Mức rủi ro càng cao thì bộ case phải càng chặt. Mức thấp có thể cho
phép hệ thống tự duyệt các cổng Design, Cases và từng bước khi nộp sạch. BRD và Review thì luôn do dev duyệt.

**5. CI là lớp thực thi cuối cùng.** Job CI chạy lại eval trên MR: build, `go test`, bộ case, độ phủ dòng mới, review
của subagent. Ở chế độ duyệt trên web, CI còn hỏi server xem các cổng đã được duyệt chưa, và **fail nếu code đổi sau
lần duyệt Review**.

## Mô hình an toàn nhiều tầng

AISDLC xây lớp bảo vệ theo nhiều tầng, mỗi tầng do code tất định đảm nhận:

- **Hook chặn** ghi vào file trạng thái và tài liệu đã duyệt, ghi ra ngoài phạm vi, mọi tool chạm thư mục chứa token
  `~/.config/aisdlc`, lệnh in biến môi trường. Hook cũng chặn agent tự chạy lệnh duyệt: lệnh duyệt đòi console thật và
  bắt gõ lại mã tính năng.
- **Hook không bao giờ gọi mạng.** Hook chỉ đọc một cache được server ký bằng Ed25519, với public key đã ghim trên máy
  dev. Nhờ vậy hook nhanh: E2E kiểm p95 độ trễ dưới 50 ms. Hook cũng không thể bị đánh lừa bằng một server giả.
- **Token có kiểu** (`login`, `ingest`, `approve`, `ci`). Token mà CLI và hook dùng không thể duyệt gì.
- **GitLab là tầng thực thi cuối cùng:** nhánh được bảo vệ cùng job eval trong CI kiểm lại mọi MR trước khi merge.

Tinh thần chung giống điều Stripe và các bài viết về harness nhấn mạnh: việc gì phải luôn đúng thì giao cho code tất
định (hook, linter, CI), không giao cho LLM.

## Một ví dụ ngắn

Hướng dẫn sử dụng đi trọn một tính năng mẫu, PAY-1 "Kiểm tra hạn mức ngày":

```bash
/aisdlc:init PAY-1        # agent tạo tính năng
/aisdlc:brd               # agent soạn brd.md, nộp cổng BRD
aisdlc approve brd        # dev, ở terminal riêng, gõ lại mã PAY-1
/aisdlc:design            # design.md, design.yaml, migration nháp; subagent xếp loại rủi ro
aisdlc approve design
/aisdlc:cases             # cases.yaml; khi nộp, harness xác nhận có case đang fail
aisdlc approve cases
/aisdlc:step S1           # agent chỉ ghi được file trong phạm vi S1
aisdlc step accept S1
/aisdlc:review            # eval + review độc lập
aisdlc approve review     # rồi mới mở MR
```

Mọi lệnh duyệt đều là việc của dev. Nếu repo bật `sessionApprove`, dev duyệt ngay trong Claude Code nhưng vẫn phải tự
bấm Yes trong hộp xác nhận quyền.

## Web: bảng tính năng và điều khiển agent (tùy chọn)

`aisdlc-web` là thành phần tùy chọn. Nó có bảng tính năng, xem trực tiếp agent đang chạy, duyệt cổng trên web, tạm
dừng agent, gửi chỉ dẫn, accept bước hay cho phép file từ trình duyệt. Trang chính sách rủi ro cho phép quản trị chỉnh
luật và xuất bản phiên bản mới.

![Bảng tính năng trên web](../images/web-bang.png)

## Song ngữ: tiếng Anh và tiếng Việt

Bản mới nhất hỗ trợ cả tiếng Anh và tiếng Việt. Mỗi repo chọn một ngôn ngữ bằng khóa `locale: en|vi` trong
`aisdlc/config.yaml`. CLI, hook, template, review card, báo cáo eval và tài liệu agent viết đều theo ngôn ngữ đó. Trên
web, mỗi người dùng tự chọn giao diện EN/VI. Repo cũ không có khóa `locale` vẫn chạy y như trước, đến từng byte.

Việc chuyển ngữ cũng được làm theo đúng tinh thần của dự án. Có test quét toàn repo để chặn chuỗi tiếng Việt "trần"
lọt ra ngoài catalog dịch, và E2E chạy thêm một lượt trên repo tiếng Anh, khẳng định output không còn ký tự tiếng Việt.

## AISDLC dành cho ai

AISDLC dành cho các team viết dịch vụ Go với PostgreSQL và goose, dùng Claude Code, và cần một quy trình kiểm soát
được: ngân hàng, thanh toán, hay bất kỳ nơi nào một thay đổi DB sai có thể gây hậu quả thật.

Kết quả của METR và DORA gợi ý rằng phần tốn công nhất khi làm với AI nằm ở kiểm chứng, không nằm ở gõ code. AISDLC
dồn công sức vào đúng chỗ đó, và sớm nhất có thể: duyệt tài liệu và bộ case trước, để mỗi bước code sau đó đã có
thước đo rõ ràng.

## Bắt đầu

```bash
git clone https://github.com/hadtinfo/aisdlc-duck.git ~/aisdlc && ~/aisdlc/plugin/install.sh
```

Trong Claude Code:

```
/plugin marketplace add ~/aisdlc
/plugin install aisdlc@aisdlc
```

Rồi chạy `aisdlc setup --locale vi` (hoặc `en`) ở gốc repo dịch vụ. Chi tiết xem
[README](../../README.vi.md), [hướng dẫn sử dụng](../huong-dan-su-dung.md) và
[trang dự án](https://hadtinfo.github.io/aisdlc-duck/). Dự án dùng giấy phép MIT, và đóng góp được chào đón.

## Tài liệu tham khảo

- Stack Overflow, *2025 Developer Survey: trust in AI at an all-time low*,
  <https://stackoverflow.co/company/press/archive/stack-overflow-2025-developer-survey/>
- Google, *How are developers using AI? Inside our 2025 DORA report*,
  <https://blog.google/technology/developers/dora-report-2025/>; InfoQ,
  <https://www.infoq.com/news/2025/09/dora-state-of-ai-in-dev-2025>
- InfoWorld, *AI coding tools can slow down seasoned developers by 19%* (về nghiên cứu của METR),
  <https://www.infoworld.com/article/4020931/ai-coding-tools-can-slow-down-seasoned-developers-by-19.html>
- Anthropic, *System Card: Claude Opus 4 & Claude Sonnet 4* (mục reward hacking, special-casing),
  <https://www.anthropic.com/claude-4-system-card>
- GitHub Blog, *Spec-driven development with AI: Get started with a new open source toolkit*,
  <https://github.blog/ai-and-ml/generative-ai/spec-driven-development-with-ai-get-started-with-a-new-open-source-toolkit/>
- Claude Code docs, *Hooks reference*, <https://code.claude.com/docs/en/hooks>
- Alistair Gray (Stripe), *Minions: Stripe's one-shot, end-to-end coding agents* (09/02/2026),
  <https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents>, và *Part 2* (19/02/2026),
  <https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents-part-2>
- Nic Gould (XMPro), *Your AI Agent Needs a Harness — Here's What That Means for Industrial Operations* (19/05/2026),
  <https://xmpro.com/your-ai-agent-needs-a-harness-heres-what-that-means-for-industrial-operations/>; bài của nhà cung
  cấp sản phẩm harness
