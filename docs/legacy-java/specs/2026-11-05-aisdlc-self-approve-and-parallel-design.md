# AISDLC: dev tự duyệt trong Claude Code và làm song song nhiều tính năng

Ngày: 2026-11-05 · Trạng thái: nháp, chờ dev duyệt

## 1. Vì sao

Hai điểm đau dev gặp khi dùng thật:

1. **Phải đổi terminal để duyệt.** Mọi lệnh chỉ dev (`approve`, `reject`, `reopen`, `step accept`, `scope allow`,
   `risk set`) đòi console thật (`Context.confirmDev`/`askDev`, exit 3 nếu không có), nên không chạy được trong
   Claude Code. Dev muốn duyệt ngay trong session đang làm, và vẫn có thể duyệt trên web.
2. **Nhiều tính năng song song bị lẫn.** Cả thư mục làm việc chỉ có một file `.aisdlc/active`
   (`ActiveFeature`). Dev đã gặp: hai session Claude Code cùng thư mục, cùng nhánh, `init PAY-1` rồi `init PAY-2`
   → `submit` ở session PAY-1 (và ở terminal) đi vào PAY-2; hook kiểm tra scope theo PAY-2.

Thành công là: dev mở nhiều session Claude Code trong một repo, mỗi session một tính năng, nộp và duyệt ngay
trong session đó mà không bao giờ phải tự hỏi "lệnh này tác động lên tính năng nào".

## 2. Mục tiêu / không làm / phản mục tiêu

Mục tiêu
- G1. Chế độ tùy chọn cho phép dev duyệt mọi lệnh chỉ dev ngay trong Claude Code, kể cả tính năng mức rủi ro HIGH
  (dev đã quyết).
- G2. Duyệt trên web và duyệt bằng terminal riêng vẫn dùng được song song, không đổi hành vi.
- G3. Mỗi session Claude Code gắn một tính năng riêng; lệnh `/aisdlc:*`, `aisdlc …` agent chạy, và hook trong
  session đó đều dùng tính năng của session.
- G4. Lệnh ở terminal ngoài không bao giờ đoán ngầm tính năng khi có nhiều tính năng đang mở.
- G5. Mọi output lệnh cho dev biết đang tác động lên tính năng nào.

Không làm
- Không tách code của nhiều tính năng trên cùng một nhánh (diff/MR vẫn chung). Chỉ cảnh báo, khuyên worktree.
- Không đổi web (snapshot đã theo từng tính năng), ngoài kênh duyệt mới `claude-code` (migration + hiển thị, mục
  3.4).
- Không đổi cách CI chấm.
- Không hỗ trợ Codex ở thiết kế này (để scope sau). Ghi chú cho sau: Codex có hook với `session_id`,
  `permission_mode` nhưng chưa hỗ trợ `ask` và không có `CLAUDE_ENV_FILE`; chỉ làm cho Claude Code.

Phản mục tiêu (làm thế này là hỏng dù chạy được)
- Agent tự duyệt được mà dev không thấy một hộp xác nhận nào.
- Repo không bật chế độ mới mà hành vi duyệt thay đổi.
- Session A âm thầm đổi tính năng của session B.

## 3. Phần 1: duyệt trong Claude Code

### 3.1. Bật

Khóa mới trong `aisdlc/config.yaml`, độc lập với `approvals: local|web` (nơi lưu bản duyệt):

```yaml
sessionApprove: true   # mặc định false
```

Như mọi sửa cấu hình, phải ở MR riêng (eval đã đánh fail thay đổi kèm sửa config).

*(Quyết định của người viết.)* Hook và CLI đọc `sessionApprove` từ bản `config.yaml` ở `baseRef` (đã có
`RepoConfig` đọc từ nội dung base tree), không từ thư mục làm việc. Bật chế độ chỉ có hiệu lực sau khi MR cấu hình
đã merge; agent sửa config tại chỗ không bật được. `baseRef` không đọc được (chưa fetch) → coi như `false`.

### 3.0. Mô hình đe dọa

Như mục "Mô hình an toàn" của hướng dẫn sử dụng: agent có thể nhầm, không giả định agent cố ý tấn công. Dù vậy, các
chốt dưới đây phải chặn được cả đường vòng *dễ vô tình gặp*: dev bấm No rồi agent thử lại, agent gọi lệnh qua biến
hoặc `bash -c`.

### 3.2. Cơ chế xác nhận của người (đã chọn)

Dùng hộp xác nhận quyền của chính Claude Code làm "console thật":

1. Agent (hoặc dev qua slash command mới) chạy lệnh chỉ dev qua Bash, ví dụ `aisdlc approve brd`.
2. Hook `PreToolUse` (matcher Bash) nhận ra lệnh chỉ dev. Khi `sessionApprove: true`:
   - nếu `permission_mode` trong input hook là chế độ không hỏi người (`bypassPermissions`, hoặc chế độ tự chấp
     nhận tương đương) → **deny**, kèm lời nhắc duyệt ở terminal hoặc web;
   - ngược lại → trả `permissionDecision: "ask"` với lý do hiển thị cho dev: tính năng, cổng, hash, và tóm tắt
     review card (số cờ HIGH/WARN), để dev quyết trong hộp Yes/No của Claude Code.
   - Đồng thời hook ghi một **phiếu cho phép một lần** vào vùng hook bảo vệ (`.aisdlc/…`, agent không ghi được),
     gắn với: session id, feature key, chuỗi lệnh đã chuẩn hóa, hash artifact hiện tại, hạn ngắn.
3. CLI chạy lệnh chỉ dev mà không có console: nếu `sessionApprove: true` và có phiếu khớp (cùng session, lệnh,
   tính năng, hash, còn hạn) → tiêu thụ phiếu (xóa) và thực hiện, **không** bắt gõ lại feature key (hộp Yes/No
   thay cho bước đó). Không có phiếu khớp → exit 3 như hiện nay.
4. `sessionApprove: false` (mặc định): hook giữ hành vi cũ, CLI giữ `confirmDev`.

Rủi ro dev chấp nhận: dev bấm Yes mà không đọc. Chế độ bỏ qua quyền bị chặn ở bước 2 nên không có đường tự duyệt
im lặng.

Chốt thêm *(quyết định của người viết)*:
- Phiếu ghi trước khi dev trả lời, nên dev bấm No thì phiếu vẫn còn. Giới hạn: hạn 60 giây; phiếu chỉ khớp đúng
  lệnh dev-only (động từ + đối số) mà hook đã nhận ra; lần `PreToolUse` kế tiếp của session (bất kỳ tool nào) xóa
  phiếu chưa dùng của session đó. Agent thử lại sau khi bị No sẽ gặp hộp xác nhận mới.
- Hook **deny** mọi lệnh Bash có chữ `aisdlc` đi kèm động từ dev-only mà hook không phân tích chắc chắn được (qua
  biến, `eval`, `bash -c`, command substitution…), dùng cùng hướng với `BashHeuristics` hiện có. Thông báo: "gọi
  thẳng `aisdlc <lệnh>` để hiện hộp xác nhận".
- Hook deny lệnh Bash gán hoặc export `AISDLC_SESSION` (mục 4.1).

### 3.3. Hiển thị cho dev

- Slash command mới `/aisdlc:approve <gate>`, `/aisdlc:reject <gate> <lý do>`, `/aisdlc:accept <Sx>`
  (tên có thể gộp, builder quyết): agent chạy `aisdlc review <gate>` để in review card vào chat, rồi chạy lệnh duyệt
  → hộp xác nhận hiện ra.
- Các slash command hiện có (`brd`, `design`, `cases`, `step`, `review`) ở bước "Dừng" đổi lời nhắc khi
  `sessionApprove: true`: "gõ `/aisdlc:approve brd` hoặc duyệt trên web" thay cho "terminal riêng". Agent vẫn
  **không tự gọi** lệnh duyệt khi dev chưa yêu cầu (quy tắc trong prompt; hộp xác nhận là chốt chặn thật).

### 3.4. Ghi nhận

- Bản duyệt có kênh `claude-code` (cạnh `terminal`, `web`, `auto`). `gates.json` (local) lưu kênh; `status`,
  review card, trang web hiện "duyệt trong Claude Code".
- Server *(quyết định của người viết)*: migration mới thêm `claude-code` vào CHECK của `approvals.channel`.
  Request duyệt bằng token approve được gửi kèm gợi ý kênh `claude-code`; server chỉ chấp nhận gợi ý này thay
  cho `terminal` (không cho nâng thành `web`). Kênh chỉ để hiển thị và audit, không phải chốt an toàn: người giữ
  token approve vốn đã là dev. Server cũ không biết trường này thì bỏ qua và ghi `terminal`, nên CLI và server
  phát hành lệch nhau vẫn chạy.
- Chế độ `approvals: web`: CLI vẫn dùng token approve như `aisdlc approve` ở terminal hiện nay; chỉ khác bước xác
  nhận người.

## 4. Phần 2: tính năng theo session

### 4.1. Gắn tính năng vào session

- Hook `SessionStart` đọc `session_id` và đưa nó vào môi trường Bash của session (cơ chế `CLAUDE_ENV_FILE` của
  Claude Code), ví dụ `AISDLC_SESSION=<id>`. CLI đọc biến này; hook đã có `session_id` trong input. Ghi biến
  **luôn** (khi có `session_id` và `CLAUDE_ENV_FILE`), kể cả khi session mở ngoài repo AISDLC, để `aisdlc use` sau đó
  gắn đúng session mà hook đọc. `init`/`use` chạy trong Claude Code (`CLAUDECODE`) mà thiếu `AISDLC_SESSION` thì
  cảnh báo (gắn kết không theo session; khởi động lại session hoặc dùng `--feature`).
- Lưu ánh xạ `session → feature key` trong thư mục làm việc, ví dụ `.aisdlc/sessions/<id>` (đã nằm trong vùng hook
  chặn agent ghi trực tiếp).
- `aisdlc init <KEY>` / `aisdlc use <KEY>` trong một session ghi ánh xạ của session đó **và** `.aisdlc/active`
  (giữ tương thích terminal và công cụ cũ).
- Phân giải tính năng (CLI và hook, cùng một hàm):
  1. key ghi rõ trên lệnh (`--feature KEY` hoặc tham số, mục 4.2);
  2. ánh xạ của session hiện tại;
  3. không ghi key và (không có session **hoặc** session chưa gắn): áp quy tắc 4.2 — đúng một tính năng đang mở
     thì dùng nó; từ hai trở lên thì hỏi hoặc exit 2; không có thì `.aisdlc/active` như hiện nay;
  4. không có → lỗi như hiện nay.
  Hook không hỏi được: session chưa gắn mà có ≥ 2 tính năng đang mở → hook chặn lệnh ghi và nhắc agent chạy
  `aisdlc use <KEY>` (agent hỏi dev nếu không biết key).
- `/clear` tạo `session_id` mới nên mất ánh xạ *(quyết định của người viết)*: không tự mang sang; rơi vào quy tắc
  trên. `resume` giữ `session_id` nên giữ ánh xạ.
  Tính năng phân giải được mà không có trên nhánh hiện tại → lỗi rõ như `Context.feature()` hiện có.

### 4.2. Terminal ngoài session

- Mọi lệnh theo tính năng nhận `--feature KEY` (hoặc `-f`).
- Không có session, không ghi key, mà có ≥ 2 tính năng "đang mở" trong thư mục (đã init, cổng REVIEW chưa duyệt)
  → lệnh **không** dùng `.aisdlc/active`: có console thì hỏi chọn từ danh sách, không console thì exit 2 kèm danh
  sách và gợi ý `--feature`.
- Đúng một tính năng đang mở → dùng nó như hiện nay.
- "Đang mở" *(quyết định của người viết, sửa sau review cuối)*: đủ cả ba điều kiện — (1) có
  `aisdlc/features/<KEY>/feature.yaml` trong checkout hiện tại (tức là thuộc nhánh đang checkout, vì thư mục này theo
  git); (2) `aisdlc/features/<KEY>/feature.yaml` **chưa** có ở `baseRef` (tính năng đã merge coi như đóng; một lần
  `git ls-tree` cho mỗi lần phân giải, xét trước để tính năng đã đóng khỏi tính hash cổng); (3) cổng REVIEW chưa được
  duyệt. Không đọc được `baseRef` (thiếu ref, clone chưa fetch...) thì bỏ điều kiện (2), chỉ xét (1) và (3). Điều kiện
  (2) cần cho repo `approvals: web`: tính năng đồng đội đã merge, hoặc trên clone mới, không có cache phong bì ký nên
  REVIEW đọc như chưa duyệt. Hệ quả: tính năng đã merge mà vẫn làm tiếp (follow-up) không được đếm là đang mở, cần
  `aisdlc use` hoặc `--feature`. Tính năng bỏ dở không cần lệnh đóng mới: dev xóa thư mục tính năng ở nhánh đó, hoặc
  dùng `--feature`.

### 4.3. Luôn nói rõ tính năng

- Dòng đầu output của mọi lệnh theo tính năng: `[PAY-1]`. Lệnh duyệt và hộp xác nhận ở 3.2 luôn nêu key.
- `aisdlc status` không tham số trong terminal ngoài session: liệt kê mọi tính năng đang mở và session nào đang gắn
  (nếu biết).

### 4.4. Cùng nhánh nhiều tính năng

Cho phép. Khi `init`/`use` làm thư mục có ≥ 2 tính năng đang mở trên cùng nhánh: cảnh báo một lần rằng diff, bước
và MR sẽ lẫn, khuyên `git worktree`. Không chặn.

### 4.5. Web và hook

- Event và snapshot đã mang `session_id` và feature key; dùng key phân giải theo 4.1 thay cho `.aisdlc/active`.
- Lệnh từ web (accept bước, cho phép file, tạm dừng) áp dụng theo feature key, không theo session; giữ nguyên.

## 5. Tương thích

- Repo không bật `sessionApprove` và chỉ làm một tính năng: hành vi như cũ, kể cả khi bản CLI mới.
- `.aisdlc/active` vẫn được ghi và đọc như phương án cuối.
- Bản ghi duyệt cũ không có kênh `claude-code`; đọc như hiện nay.

## 6. Kiểm thử (tối thiểu)

- Hook: lệnh chỉ dev + `sessionApprove` + permission mode thường → `ask` và tạo phiếu; + bypass → deny; tắt chế độ →
  hành vi cũ.
- CLI: phiếu khớp → chạy một lần, phiếu bị tiêu thụ; lệch session/lệnh/hash/hết hạn → exit 3.
- Hai session cùng thư mục: init PAY-1 / PAY-2, submit và hook scope mỗi session đúng tính năng của mình.
- Terminal ngoài session với 2 tính năng mở: không có `--feature` → hỏi hoặc exit 2.
- e2e `e2e/run-e2e.sh`: thêm kịch bản hai session song song và duyệt trong session.

## 7. Để builder quyết

- Tên chính xác slash command và cờ (`--feature`/`-f`), định dạng file phiếu và ánh xạ session, thời hạn phiếu.
- Cách chuẩn hóa chuỗi lệnh để so khớp phiếu (bỏ khoảng trắng thừa, đường dẫn launcher…).
- Dọn ánh xạ session cũ (ví dụ khi `SessionEnd` hoặc quá hạn).
- Danh sách chính xác giá trị `permission_mode` coi là "không hỏi người", theo bản Claude Code đang hỗ trợ.
