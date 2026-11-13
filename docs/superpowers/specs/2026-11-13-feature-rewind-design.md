# Quay về mốc (`aisdlc rewind`)

Ngày: 2026-11-13. Trạng thái: chờ duyệt.

## Mục đích

Hiện nay muốn làm lại một tính năng đang dở, dev phải tự ghép nhiều thao tác (`git restore`, `aisdlc reopen`, xoá
branch, init lại) và có những trạng thái không quay lui được:

- Bước đã `ACCEPTED` không có lệnh nào bỏ accept.
- `aisdlc reopen design` không đụng `steps.json`, nên thiết kế đổi rồi mà các bước cũ vẫn ACCEPTED.
- Không có cách huỷ tính năng; server web vẫn giữ bản ghi cũ theo key.
- Làm lại bước chưa accept (`step start Sx` lần nữa) thì code vẫn phải tự revert.

Mục tiêu: một thao tác duy nhất, giống `/rewind` của Claude Code: **chọn một mốc, mọi thứ sinh ra sau mốc đó
(artifact, trạng thái cổng/bước, code) biến mất như chưa từng có**, và dev làm lại từ đó với cùng feature key. Ở chế
độ web, lần làm cũ không mất mà vào mục lưu trữ (archive) để xem lại.

## Quyết định của chủ dự án

- Gom mọi kiểu làm lại vào **một lệnh quay về mốc** (đã cân nhắc các cách khác, xem "Các cách đã cân nhắc").
- Quay về là **xoá sạch về trang trắng**: artifact sau mốc bị xoá, không giữ làm bản nháp. Mốc xa nhất là **đầu BRD**.
- **Chỉ chạy ở CLI trên máy dev.** Web không có nút quay về; web chỉ hiển thị việc đã quay về.
- Ở chế độ web, **nội dung lần làm cũ đã gửi lên server được giữ** và xem được trong phần **archive** trên web.
- Code quay về **giống hệt `/rewind` của Claude Code**: cả working tree về đúng snapshot của mốc, kể cả thay đổi không
  do tính năng này tạo ra (code pull/merge từ `main`, sửa tay, code của tính năng khác). Ref dự phòng là lưới an toàn.

## Mục tiêu

1. `aisdlc rewind [mốc]` quay tính năng đang làm (hoặc `-f KEY`) về một mốc: không có tham số thì hiện danh sách mốc để
   chọn; có tham số thì nhảy thẳng.
2. Trước khi làm, lệnh in **bản xem trước** những gì sẽ mất, rồi xác nhận như các lệnh chỉ-dev khác.
3. Sau lệnh, trạng thái luôn nhất quán: không còn cổng/bước/artifact nào sinh sau mốc; code đúng bằng snapshot của mốc.
4. Không mất dữ liệu ngoài ý muốn: bản hiện tại được cất vào ref dự phòng trên máy dev trước khi xoá.
5. Ở chế độ web: server ghi nhận lần quay về, khép "lần làm" hiện tại vào archive, mở lần làm mới; trang tính năng có
   mục xem các lần làm trước (chỉ đọc).

## Không làm (non-goals)

- Không có nút quay về trên web, kể cả cho lead.
- Không có lệnh huỷ/xoá hẳn tính năng (`--abandon`); `rewind brd` thay thế.
- Không viết lại lịch sử git (không `reset --hard`, không sửa commit đã có). Quay về chỉ đổi working tree; thay đổi hiện
  ra như sửa chưa commit.
- Không đưa code của lần làm cũ lên server; archive chỉ có artifact đã nộp, trạng thái và thống kê diff.
- Không có "redo" (quay tới lại). Cứu bản cũ bằng ref dự phòng là thao tác git thủ công, có hướng dẫn.

## Điều cấm (anti-goals)

- Agent chạy được `rewind`, hoặc lách được qua Bash. Lệnh này xoá code; nó phải là lệnh chỉ-dev như `reopen`.
- Quay về nửa chừng: lỗi giữa chừng để lại trạng thái lai (ví dụ cổng đã xoá mà code chưa về). Phải all-or-nothing
  theo nghĩa thực tế (xem "Thứ tự thực hiện").
- Ở chế độ web, máy dev đã xoá mà server chưa biết, hoặc ngược lại.
- Mất lịch sử duyệt: dòng "ai duyệt/trả/quay về lúc nào" là dấu vết kiểm toán, không bị xoá ở đâu cả.

## Hiện trạng liên quan (đã đọc code)

- Trạng thái tính năng nằm trong `aisdlc/features/<KEY>/` (`internal/model/paths.go`): `feature.yaml`, `brd.md`,
  `design.md`, `design.yaml`, `db/` (migration nháp), `schema-expected.json`, `cases.yaml`, `risk.yaml`, `gates.json`,
  `steps.json`, `scope-extra.json`, `feedback.md`, `eval/`, `review-agent.json`, và các file kết quả risk.
- Luồng cổng: đầy đủ `BRD → DESIGN → CASES → REVIEW`, nhẹ `CASES → REVIEW` (`gate.Sequence`). Bước S1…Sn khai trong
  `design.yaml` (luồng nhẹ: `cases.yaml`).
- Code sản phẩm chỉ được ghi trong các bước (hook chặn trước khi qua cổng case). Vì vậy **mọi mốc từ đầu BRD tới đầu S1
  có cùng một trạng thái code**: snapshot đầu S1.
- `step start` lưu `StartTree` = `git SnapshotTree` (tree object tạo qua index tạm, `internal/gitx/repo.go`). **Tree
  này không có ref nào trỏ tới, nên `git gc` có thể xoá sau thời hạn prune.** Đây là ràng buộc của thiết kế.
- `step start` trên bước `AWAITING_DEV` mở lại bước và giữ `StartTree` cũ.
- `reopen` local: `LocalGateStore.Clear(g)` xoá submission/approval của đúng cổng đó, ghi `feedback.md`.
- `reopen` web: `gates.Service.feedback` thu hồi (`revoked_at`) approval của cổng và **mọi cổng sau**, ghi feedback,
  sinh chỉ dẫn cho agent, audit. Bản nộp (`submissions`, có `artifacts` jsonb đầy đủ nội dung) không bao giờ bị xoá.
- Bảng `features` giữ **một** snapshot mới nhất cho mỗi `(repo_id, feature_key)`; lần đồng bộ sau ghi đè.
- `events` bị dọn sau 180 ngày (`ingest.RetentionDays`).
- Xác nhận chỉ-dev: `Context.ConfirmDev` (console thật, gõ lại key, hoặc `sessionApprove`); hook phân loại lệnh
  chỉ-dev trong `internal/cli/hook_ask.go` và `internal/policy`.
- Ở chế độ web, máy dev đọc trạng thái cổng từ cache có chữ ký `.aisdlc/web/gates/<KEY>.json`.

## Các cách đã cân nhắc

| Cách | Mô tả | Được | Mất |
|---|---|---|---|
| **A. Một lệnh quay về mốc** (chọn) | `rewind <mốc>` xử lý dây chuyền cổng, bước, artifact, code | Dev nhớ một lệnh; trạng thái luôn nhất quán; giống `/rewind` quen thuộc | Phần việc lớn nhất, đụng CLI, store, web |
| B. Vá bằng lệnh nhỏ | `step reset`, `reopen --reset-steps`, `feature abandon`; code tự revert | Nhanh, mỗi lệnh đơn giản | Dev phải ghép đúng thứ tự; dễ quên revert code làm lẫn diff |
| C. Dựa vào git | Mỗi tính năng một branch; chỉ thêm `abandon` để dọn web | Rẻ nhất | Không cứu được "thiết kế sai giữa chừng nhưng muốn giữ BRD" |

Về artifact sau mốc, đã cân nhắc: giữ làm bản nháp, xoá sạch, hoặc cờ `--clean`. Chủ dự án chọn **xoá sạch**.

Về code, đã cân nhắc:
(1) chỉ đảo các file mà các bước bị bỏ đã sửa, dừng khi file đó cũng bị đổi từ nguồn khác;
(2) cả working tree về đúng snapshot mốc;
(3) như (2) nhưng từ chối chạy khi có thay đổi từ ngoài.
Chủ dự án chọn **(2)**, giống `/rewind` của Claude Code.

## Thiết kế

### Mốc

Mỗi tính năng có chuỗi mốc theo luồng của nó:

- Luồng đầy đủ: `brd`, `design`, `cases`, `S1` … `Sn`, `review`.
- Luồng nhẹ: `cases`, `S1` … `Sn`, `review`.

"Quay về mốc X" nghĩa là đưa tính năng về đúng lúc **bắt đầu** X. Chỉ hiện các mốc đã đạt tới (không quay "về" một
mốc chưa từng bắt đầu). Bảng xoá theo mốc:

| Mốc | Cổng bị xoá trạng thái | Artifact bị xoá | Bước | Code về |
|---|---|---|---|---|
| `brd` | BRD và mọi cổng sau | `brd.md`, `design.md`, `design.yaml`, `db/`, `schema-expected.json`, `cases.yaml`, `risk.yaml` và file kết quả risk, `eval/`, `review-agent.json`, `scope-extra.json` | xoá hết | snapshot đầu S1 |
| `design` | DESIGN và sau | như trên trừ `brd.md` | xoá hết | snapshot đầu S1 |
| `cases` | CASES và sau | `cases.yaml`, `risk.yaml`* và kết quả risk*, `eval/`, `review-agent.json`, `scope-extra.json` (luồng nhẹ thêm `schema-expected.json`) | xoá hết | snapshot đầu S1 |
| `Sk` | REVIEW | `eval/`, `review-agent.json`, mục của Sk…Sn trong `scope-extra.json`** | xoá Sk…Sn | snapshot đầu Sk |
| `review` | REVIEW | `eval/`, `review-agent.json` | giữ | không đổi |

\* Mức rủi ro được xếp lúc `submit design` (luồng đầy đủ), nên quay về `cases` ở luồng đầy đủ **giữ** `risk.yaml`;
ở luồng nhẹ thì xoá nếu nó được sinh trong cổng case. Builder xác nhận lại theo `internal/risk` và `gate.Artifacts`.

\*\* Hiện `ScopeEntry` (`internal/step/extras.go`) chỉ có `{path, approver, at}`, không biết thuộc bước nào. Thêm
trường `step` (bước đang mở lúc `scope allow`; rỗng nếu không có) từ nay. Khi quay về `Sk`: xoá mục có `step` ≥ Sk;
**giữ** mục cũ không có `step` (quyết định của người viết: thà dư quyền dev đã cấp còn hơn mất quyền).

Luôn giữ: `feature.yaml` (key, luồng, owner), `feedback.md` (được ghi thêm một dòng quay về).

Chưa có bước nào được bắt đầu thì mọi mốc cổng không đổi code.

### Giữ snapshot đầu bước khỏi bị `git gc`

`step start` ghi thêm ref `refs/aisdlc/checkpoints/<KEY>/<Sx>` trỏ tới một commit bọc `StartTree` (git chỉ giữ object
qua ref trỏ tới commit/tag/tree; dùng commit để có thông điệp và thời điểm). Ref đi theo tính năng, không push.
Tính năng cũ chưa có ref: `rewind` vẫn chạy nếu tree còn trong kho; mất tree thì báo rõ và chỉ cho quay về mốc còn
snapshot (hoặc mốc cổng nếu S1 cũng mất thì báo không khôi phục được code, cho phép quay về chỉ trạng thái nếu dev
xác nhận thêm).

### Lệnh

```
aisdlc rewind              # hiện danh sách mốc để chọn
aisdlc rewind <mốc>        # brd | design | cases | S1..Sn | review (không phân biệt hoa thường)
aisdlc rewind <mốc> -f KEY
```

Danh sách và bản xem trước (ví dụ ROOM-1, luồng đầy đủ, S1–S3 đã accept):

```
? Quay về mốc nào (ROOM-1)
  1. Đầu BRD       — xoá: brd.md, design.*, db/, cases.yaml, risk, code S1–S3
  2. Đầu Thiết kế  — xoá: design.*, db/, cases.yaml, risk, code S1–S3
  3. Đầu Case      — xoá: cases.yaml, code S1–S3
  4. Đầu S1        — xoá: code S1–S3
  5. Đầu S2        — xoá: code S2–S3
  6. Đầu S3        — xoá: code S3
  7. Đầu Review    — xoá: kết quả eval, review

Sẽ quay ROOM-1 về đầu S2:
  - Bỏ bước S2, S3 (ACCEPTED)
  - Mở lại cổng Review
  - Code về snapshot đầu S2: 7 file đổi, 2 file mới bị xoá
  - Bản hiện tại cất ở refs/aisdlc/backup/ROOM-1/20261113-1405
Gõ lại feature key (ROOM-1) để xác nhận:
```

- Xác nhận đúng cơ chế `ConfirmDev` (console thật hoặc `sessionApprove`). Không có cờ bỏ qua xác nhận.
- Thêm `rewind` vào danh sách lệnh chỉ-dev của hook (cùng nhóm `reopen`), để agent gọi qua Bash bị chặn.
- Có plugin skill `/aisdlc:rewind` hay không: chỉ khi `sessionApprove` bật, giống `/aisdlc:approve`. Builder quyết theo
  cách các skill duyệt hiện có.
- In kết quả và bước tiếp theo: "Đã quay ROOM-1 về đầu S2. Tiếp: `aisdlc step start S2`." (tương ứng cho từng mốc).

### Code

- Tính tập file khác nhau giữa snapshot mốc và snapshot hiện tại của toàn working tree, trừ:
  - thư mục `aisdlc/features/` (trạng thái của tính năng này xử lý riêng ở bước xoá artifact; **thư mục của tính năng
    khác không bao giờ bị đụng**, để trạng thái cổng/bước của chúng không bị đảo ngầm), và
  - các path bị bỏ qua theo `step.Ignored` và `.aisdlc/`.
- Với mỗi file trong tập: có trong snapshot mốc thì ghi lại nội dung đó; không có thì xoá. Kết quả: phần code của
  working tree đúng bằng snapshot mốc (quyết định của chủ dự án, cách (2)).
- **Bản xem trước tách hai nhóm** để dev thấy mình mất gì: (a) file do các bước bị bỏ của tính năng này đổi (từ
  `Changes` của Sk…Sn và diff của bước đang dở); (b) file đổi **ngoài** các bước đó (merge/pull, sửa tay, tính năng
  khác). Nhóm (b) in cảnh báo riêng, kèm ghi chú nếu `HEAD` đã đổi kể từ mốc và nếu có tính năng khác đang có bước
  dở. Không chặn; ref dự phòng cứu được.
- Không đổi `HEAD`, không đổi lịch sử git. Nếu dev đã commit code sau mốc, sau quay về các thay đổi hiện ra là sửa
  chưa commit; dev tự commit.

### Ref dự phòng

Trước khi xoá bất cứ gì, tạo commit chứa snapshot hiện tại của **toàn bộ** working tree (gồm cả thư mục tính năng) và
ghi `refs/aisdlc/backup/<KEY>/<yyyymmdd-hhmm>`. In tên ref. Hướng dẫn cứu (trong docs): `git restore --source <ref> -- <path>`.
Không tự dọn ref dự phòng; docs chỉ cách xoá.

### Thứ tự thực hiện

1. Kiểm tra: tính năng tồn tại, mốc hợp lệ và đã đạt tới, snapshot mốc còn, không có bước nào đang `IN_PROGRESS` của
   phiên agent khác (nếu có thì vẫn cho, nhưng xem trước ghi rõ).
2. Xem trước, xác nhận.
3. Tạo ref dự phòng.
4. Chế độ web: gửi lệnh rewind lên server và chờ thành công. Lỗi mạng/server thì dừng ở đây, **chưa xoá gì** trên máy.
5. Đưa code về snapshot mốc.
6. Xoá artifact, cập nhật `gates.json` (local) / làm mới cache cổng có chữ ký (web), `steps.json`, `scope-extra.json`.
7. Ghi `feedback.md`: `## REWIND — quay về <mốc> bởi <người> lúc <thời điểm> (dự phòng: <ref>)`.

Lỗi sau bước 4: in rõ bước nào đã xong, bước nào chưa, và ref dự phòng; chạy lại `aisdlc rewind <mốc>` phải hoàn tất
được phần còn lại (các bước 5–7 idempotent). Server đã ghi nhận lần quay về thì lần chạy lại không tạo lần làm mới
thứ hai.

### Agent và phiên Claude Code

Sau quay về, agent nhận chỉ dẫn (cơ chế giống chỉ dẫn khi cổng bị trả): "Tính năng ROOM-1 đã được quay về đầu S2 bởi
<dev>. Mọi thứ sau mốc đã bị xoá; bắt đầu lại bằng `aisdlc step start S2`." Ở chế độ local, chỉ dẫn này đi qua
`feedback.md`/hook như hiện nay; ở chế độ web, server tạo chỉ dẫn như với reopen.

### Web

**Lần làm (attempt).** Mỗi tính năng có số lần làm, bắt đầu từ 1. Server thêm:

- Cột `attempt` (int, mặc định 1) cho `features`, `submissions`, `approvals`, `feedback`, `instructions`,
  `pending_actions` (và các bảng theo tính năng khác nếu builder thấy cần). Truy vấn trạng thái hiện tại chỉ đọc
  bản ghi của lần làm hiện tại.
- Bảng `feature_rewinds`: `repo_id`, `feature_key`, `from_attempt`, `to_attempt`, `checkpoint` (mốc), `by`,
  `at`, `snapshot` (jsonb: bản sao snapshot tính năng tại lúc quay về), `request_id` (để lần chạy lại không nhân đôi).

**Endpoint quay về** (chỉ CLI gọi, xác thực như `reopen`, người gọi phải có quyền reopen cổng đầu tiên bị ảnh
hưởng). Trong một giao dịch:

1. Chép snapshot hiện tại của `features` vào `feature_rewinds.snapshot`.
2. Thu hồi mọi approval còn hiệu lực của lần làm hiện tại thuộc các cổng từ mốc trở đi, `revoked_by` = người quay về,
   lý do "rewind".
3. Tăng `features.attempt`.
4. **Mang sang lần làm mới** các bản nộp và approval của các cổng **trước** mốc (ví dụ quay về `design` thì BRD đã
   duyệt vẫn hiệu lực ở lần làm mới). Cách làm (sao chép hàng với attempt mới hay để truy vấn đọc "attempt ≤ hiện tại
   và chưa bị khép") để builder chọn; yêu cầu là cổng trước mốc vẫn APPROVED với cùng hash.
5. Hủy các `pending_actions` của lần làm cũ còn chờ.
6. Ghi `audit` (`feature.rewind`) và tạo chỉ dẫn cho agent.
7. Trả trạng thái cổng mới có chữ ký để CLI ghi cache.

**Snapshot mang số lần làm.** Snapshot tính năng gửi lên server (ingest) thêm trường `attempt`, lấy từ cache cổng có
chữ ký trên máy dev. Server bỏ qua (không ghi đè `features`, trả mã báo "lần làm cũ") snapshot có `attempt` nhỏ hơn
lần làm hiện tại, để máy khác, bản clone khác hoặc một lần đồng bộ đang dở không ghi đè lần làm mới. Snapshot không có
trường này (client cũ) được coi là `attempt` hiện tại khi tính năng chưa từng quay về, và bị bỏ qua khi đã quay về
(quyết định của người viết). CLI nhận mã "lần làm cũ" thì làm mới cache cổng và báo dev.

**Archive trên trang tính năng.** Mục "Các lần làm trước" (chỉ hiện khi có), mỗi lần làm hiển thị chỉ đọc:

- Thời điểm, người quay về, mốc quay về.
- Theo từng cổng: các bản nộp (nội dung artifact từ `submissions.artifacts`), ai duyệt/trả và lúc nào, ghi chú.
- Trạng thái các bước và thống kê diff (file, dòng, case đạt) từ snapshot đã chép.
- Review card tại lúc quay về nếu snapshot có.

Web không có nút quay về. Board và trang tính năng chỉ hiển thị lần làm hiện tại, kèm nhãn "Lần làm 2" khi > 1 và một
dòng sự kiện "Quay về đầu S2 bởi … lúc …".

**Lưu giữ.** `feature_rewinds` và các bản ghi của lần làm cũ không bị đợt dọn `events` xoá. Sự kiện agent của lần làm
cũ vẫn theo hạn 180 ngày như hiện nay.

### Chế độ local (không web)

Như trên, trừ phần server: không có archive trên web. Lịch sử nằm trong `feedback.md` và ref dự phòng.

## Kiểm thử

- Đơn vị: tính bảng xoá theo mốc cho cả hai luồng; tính tập file khôi phục; idempotent của các bước 5–7.
- Tích hợp CLI (repo git tạm): quay về từng loại mốc, kiểm `gates.json`, `steps.json`, artifact, working tree, ref
  dự phòng và checkpoint; `git gc --prune=now` không làm mất snapshot đầu bước.
- Hook: agent gọi `aisdlc rewind` (trực tiếp và qua Bash) bị chặn; không console thì thoát 3 như `reopen`.
- Web (IT với Postgres như các `*_it_test.go`): lần làm tăng, approval sau mốc bị thu hồi, cổng trước mốc vẫn
  APPROVED, archive đọc được nội dung bản nộp cũ, chạy lại cùng `request_id` không nhân đôi, mất mạng thì máy dev
  không đổi.
- E2E: thêm kịch bản quay về đầu S2 rồi làm lại vào `e2e/`.
- Song ngữ: chuỗi mới có trong cả catalog vi/en (CLI và web UI), theo kiểm tra hiện có.

## Docs

Cập nhật `docs/huong-dan-su-dung.md` và `docs/user-guide.md`: mục "Làm lại tính năng" (mốc, xem trước, ref dự phòng,
cứu bản cũ, archive trên web), bảng lệnh, bảng xử lý sự cố. `reopen` vẫn giữ cho trường hợp chỉ sửa một artifact.

## Để builder quyết

- Định dạng thông điệp commit cho checkpoint/backup và vị trí code (gói `internal/step`, `internal/gate` hay gói mới
  `internal/rewind`).
- Cách mang cổng trước mốc sang lần làm mới ở server (mục Web, bước 4).
- Giao diện chọn mốc khi không có tham số (danh sách đánh số như ví dụ là đủ; không cần TUI).
- Bố cục mục archive trên web, theo `DESIGN.md`.
- Thêm skill `/aisdlc:rewind` hay không (mục Lệnh).
