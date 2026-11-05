# AISDLC — Chính sách rủi ro theo repo: phân loại tính năng và siết bộ case theo mức

Ngày: 2026-11-04. Prototype giao diện: https://claude.ai/artifact/11WPT7XtbgHMVxBBS21oXw (bản nháp để review, không phải
code).

Nhãn trong tài liệu:
- **[Người dùng]**: người dùng đã quyết.
- **[Lựa chọn của Claude]**: đề xuất của Claude; builder đổi được nếu có lý do.

## 1. Vì sao cần

Hiện mọi tính năng phải qua cùng một mức kiểm tra ở cổng Case: có case fail, mỗi rule BRD có case. Coverage dòng mới
chỉ là cảnh báo mềm. Trong khi đó, story chạm tiền (hạch toán, tính phí) và story sửa màn báo cáo bị đối xử như nhau.

Người dùng muốn **[Người dùng]**:
- đánh giá và phân loại rủi ro của từng story/task;
- cập nhật được các rule dùng để phân loại;
- rủi ro cao thì bộ case phải chặt hơn;
- phân loại theo hai nguồn: rule (ví dụ story hạch toán, tính phí) và đánh giá tác động hệ thống của agent;
- **generic**: mỗi dự án có code base và nghiệp vụ khác nhau, nên rule và yêu cầu do dự án tự khai, lõi AISDLC không
  gắn cứng nghiệp vụ ngân hàng;
- quản lý trên web, ở trang của repo, và dev được sửa;
- khi init dự án thì tự import chính sách mẫu từ AISDLC sang;
- ở giai đoạn này web chạy local trên máy dev, nên **dev tự duyệt**: sửa xong xuất bản là có hiệu lực ngay, không cần
  người thứ hai.

## 2. Mục tiêu, không làm, phản mục tiêu

**Mục tiêu**
1. Mỗi repo có một **chính sách rủi ro** gồm thang mức, rule nhận diện và yêu cầu case theo mức.
2. Mỗi tính năng có **mức rủi ro**, kèm nguồn: rule nào khớp, agent đánh giá gì, dev đặt tay hay không.
3. Validator ở cổng Case và eval ở cổng Review **chặn** khi thiếu yêu cầu của mức đó.
4. Web có trang quản lý chính sách với 5 phần: rule, yêu cầu theo mức, thử phân loại, phiên bản, hiển thị trên
   tính năng.
5. `aisdlc setup` chép chính sách mẫu vào repo; lần đầu repo kết nối web thì web nhập file đó làm phiên bản 1.
6. Không có web vẫn chạy được: chính sách nằm trong file của repo.

**Không làm (đợt này)**
- Bước "kế hoạch trước khi code" và "người duyệt thứ hai". Prototype có hai mục này, nhưng chúng không phải yêu
  cầu về case, và người dùng đã chọn dev tự duyệt.
- Chia sẻ chính sách giữa nhiều repo, hay chính sách cấp team. Mỗi repo một chính sách, khởi đầu từ mẫu.
- Tích hợp Jira/GitLab để lấy nhãn story (đã bỏ 2c).
- Yêu cầu tự viết dạng HTTP probe. Đợt này chỉ hỗ trợ SQL.

**Phản mục tiêu** (có những điều này thì coi như hỏng, dù code chạy)
- Lõi AISDLC (core, CLI, harness) chứa từ khóa, đường dẫn hay tên bảng của một nghiệp vụ cụ thể. Các thứ đó chỉ được
  nằm trong file mẫu (preset).
- Đổi chính sách làm tính năng đang làm dở bị đổi yêu cầu giữa chừng mà dev không biết.
- Hook phải gọi mạng để biết mức rủi ro.
- Agent tự hạ được mức rủi ro.

## 3. Khái niệm

### 3.1 Chính sách (policy) **[Lựa chọn của Claude]**

Định dạng YAML, cùng một schema cho file trong repo, cho bản lưu trên web và cho bản cache:

```yaml
schema: 1
preset: { name: banking, version: "0.3" }   # mẫu gốc; dùng để báo khi AISDLC có mẫu mới
levels: [low, medium, high]                 # thứ tự tăng dần; dự án được đổi tên/số mức (2–5 mức)
default: low                                # mức khi không có gì khớp
rules:
  - id: R1
    kind: keyword        # keyword | path | table | endpoint | label
    match: "hạch toán|bút toán|định khoản"
    level: high
    note: Mọi thay đổi chạm sổ cái
    enabled: true
  - id: R4
    kind: table
    match: ["ledger_*", "fee_*", "account_balance"]
    level: high
classification:
  agent: raise           # raise: agent chỉ được nâng mức | off: bỏ qua đánh giá của agent
  devLower: reason       # reason: dev hạ mức phải ghi lý do | never: không cho hạ
requirements:            # theo mức; mức không liệt kê = không yêu cầu gì thêm
  medium:
    - { id: rule-pos-neg }
    - { id: boundary }
    - { id: coverage, min: 75 }
  high:
    - { id: rule-pos-neg }
    - { id: boundary }
    - { id: idempotent }
    - { id: concurrent, threads: 8 }
    - { id: invariant }
    - { id: coverage, min: 90 }
    - { id: sql-zero, name: "Tổng nợ bằng tổng có",
        sql: "SELECT txn_id FROM ledger_entry GROUP BY txn_id HAVING SUM(debit) <> SUM(credit)" }
```

### 3.2 Bộ so khớp (matcher)

Năm loại bộ so khớp, dùng chung cho mọi dự án. Đây là thứ duy nhất lõi AISDLC biết.

| kind | `match` là | So với | Có từ cổng |
|---|---|---|---|
| `keyword` | regex, không phân biệt hoa thường | tiêu đề, `brd.md`, `design.md` | BRD |
| `path` | glob hoặc danh sách glob | `scope` của các bước trong `design.yaml` (luồng lite: `scope` trong `cases.yaml`) | Thiết kế |
| `table` | glob tên bảng (có thể `bảng.cột`) | bảng/cột trong migration mới và ER diff của thiết kế | Thiết kế |
| `endpoint` | `METHOD /path/**` | bảng API trong `design.md` | Thiết kế |
| `label` | một nhãn | nhãn agent gắn trong đánh giá tác động (§3.4) | Thiết kế |

Một rule khớp khi có ít nhất một phần tử khớp. Kết quả lưu lại phần tử đã khớp (ví dụ "bảng ledger_entry") để hiển thị.

Rule `path` so khớp glob với glob: rule khớp khi một glob scope và glob của rule có thể cùng khớp một đường dẫn. **[Lựa
chọn của Claude]** Dùng phép xấp xỉ: so khớp glob rule với scope sau khi thay `**` và `*` bằng một đoạn mẫu. Builder có
thể dùng cách chính xác hơn nếu có sẵn trong ScopeFlags.

### 3.3 Thư viện yêu cầu

Thư viện nằm trong lõi. Mỗi yêu cầu có id cố định, tham số và nơi kiểm. Dự án chỉ chọn yêu cầu và đặt tham số.

| id | Tham số | Ý nghĩa | Kiểm ở |
|---|---|---|---|
| `rule-pos-neg` | — | Mỗi rule `BR-n` trong BRD có ≥ 1 case `happy` và ≥ 1 case `error`/`boundary` tham chiếu nó | cổng Case (CasesValidator) |
| `boundary` | `min` (mặc định 1) | Mỗi rule BRD gắn thẻ `[biên]` có ≥ `min` case `boundary` | cổng Case |
| `idempotent` | — | Mỗi endpoint POST/PUT/PATCH/DELETE mới hoặc đổi trong bảng API có ≥ 1 case `when.repeat ≥ 2`. Không có bảng API thì cần ≥ 1 case như vậy trong cả bộ. | cổng Case; harness chạy |
| `concurrent` | `threads` | Có ≥ 1 case `when.parallel ≥ threads` | cổng Case; harness chạy |
| `invariant` | `min` (mặc định 1) | `design.yaml` khai ≥ `min` bất biến (`invariants`) | cổng Case; harness chạy sau mọi case |
| `coverage` | `min` (%) | Coverage dòng mới ≥ `min` | eval, cổng Review (thành kiểm tra cứng) |
| `sql-zero` | `name`, `sql` | Truy vấn dự án tự viết phải trả 0 dòng sau mọi case | harness chạy; eval |

Yêu cầu `sql-zero` là chỗ để dự án thêm kiểm tra nghiệp vụ riêng mà không phải sửa AISDLC.

Các yêu cầu này cần mở rộng định dạng case và thiết kế. Phần mở rộng tương thích ngược; file cũ vẫn hợp lệ:
- `when.repeat: n` (mặc định 1): gửi cùng request n lần. `then` áp cho lần cuối; `then.db` kiểm sau lần cuối.
- `when.parallel: n` (mặc định 1): gửi n request cùng lúc. `then.status` phải là danh sách trạng thái cho phép, hoặc
  dùng `then.statusCounts` (ví dụ `{200: 1, 409: 7}`). `then.db` kiểm sau khi tất cả xong.
- `design.yaml`: `invariants: [{ name, sql }]`. Mỗi truy vấn phải trả 0 dòng. Harness chạy sau mọi case, kể cả case
  `error`.
- Thẻ `[biên]` ở cuối dòng rule trong BRD, ví dụ `- BR-3: Hạn mức tối đa 500 triệu/ngày [biên]`.
- `design.yaml`: `impact` (§3.4).

**[Lựa chọn của Claude]** Tên trường `repeat`, `parallel`, `statusCounts`, `invariants`, `impact` và thẻ `[biên]` là đề
xuất. Builder giữ nguyên trừ khi đụng tên sẵn có.

### 3.4 Đánh giá tác động của agent

Ở bước thiết kế, agent ghi thêm vào `design.yaml`:

```yaml
impact:
  level: high                 # một mức trong levels của chính sách
  labels: [money-movement]    # tự do; rule kind=label so với đây
  reason: "Ghi bút toán giảm doanh thu; sai sẽ lệch sổ cái cuối ngày."
```

- `aisdlc policy show` in thang mức và danh sách nhãn mà rule `label` đang dùng, để agent chọn đúng chữ. Template lệnh
  `/aisdlc:design` nhắc agent chạy lệnh này rồi điền `impact`.
- DesignValidator báo lỗi khi thiếu `impact`, hoặc khi `level` không có trong thang.
- Luồng lite không có `design.yaml`, nên `impact` nằm trong `cases.yaml`.

### 3.5 Phân loại

```
mức = max( default,
           mức của mọi rule đang bật mà khớp,
           impact.level   nếu classification.agent = raise,
           mức dev nâng tay )
```

Dev **hạ** mức thì mức đó thay cho kết quả trên. Điều kiện: `devLower = reason` và dev ghi lý do. Agent không có lệnh
nào để hạ mức.

**Khi nào tính.** **[Lựa chọn của Claude]**
- Lúc `aisdlc submit brd`: tính **tạm** chỉ bằng rule `keyword`. Kết quả hiện trên thẻ BRD để dev biết sớm. Không lưu.
- Lúc `aisdlc submit design`: tính đầy đủ và ghi `aisdlc/features/<KEY>/risk.yaml`. Thẻ Thiết kế hiện mức để dev
  duyệt cùng thiết kế. `aisdlc submit cases` tính lại nếu chính sách hoặc thiết kế đã đổi kể từ lần trước.
- **Khóa ở cổng Case**, vì đây là cổng thực thi yêu cầu: duyệt cổng Case thì `risk.yaml` nằm trong hash cổng Case.
  Từ đó muốn đổi mức phải mở lại cổng Case.
- Chỉ CLI ghi `risk.yaml`. Hook chặn agent ghi file này ở mọi thời điểm.
- **Luồng lite** không có BRD và thiết kế, nên chỉ dùng được 3 loại:
  - `keyword`: so với tiêu đề tính năng và `title` của các case;
  - `path`: so với `scope` trong `cases.yaml`;
  - `label`: so với `impact` trong `cases.yaml`.

  Rule `table` và `endpoint` không áp dụng ở lite. Phân loại và khóa đều ở cổng Case.

**Nguồn dữ kiện** **[Lựa chọn của Claude]**: không viết parser mới, dùng lại phần đã có của review card.
- Bảng và cột lấy từ `SqlFacts`: migration mới và ER diff, cùng định dạng SQL thuần mà review card đang hỗ trợ.
- Endpoint lấy từ bảng API mà `DocFacts` đã đọc trong `design.md`.

**Không đọc được chính sách** (bật web mà cache thiếu hoặc chữ ký sai): `submit design` và `submit cases` dừng, báo
"chạy `aisdlc policy pull`". Không lặng lẽ dùng file trong repo. Hook không cần chính sách, nên không bị ảnh hưởng.
`policy.hash` trong `risk.yaml` là hash nội dung của bản chính sách đã dùng (hash của body đã ký, hoặc hash của file).

**Tên mức:** mức so theo tên, thứ tự lấy từ `levels` của chính sách mà tính năng đã dùng. `reclassify` tính lại từ đầu
theo phiên bản mới, nên không cần bảng chuyển đổi khi dự án đổi tên mức.

`risk.yaml` là bản chụp, để CI và máy khác không cần hỏi chính sách:

```yaml
level: high
policy: { version: 7, hash: "sha256:…" }
matched:
  - { rule: R2, level: high, on: "“phí” trong brd.md" }
  - { rule: R4, level: high, on: "bảng ledger_entry" }
agent: { level: high, labels: [money-movement], reason: "…" }
override: null            # hoặc { to: medium, by: <dev>, reason: "…", at: … }
requirements:             # copy nguyên các yêu cầu của mức này từ chính sách v7
  - { id: rule-pos-neg }
  - …
```

Từ đây validator, harness và eval đọc `requirements` trong `risk.yaml`, không đọc chính sách. Nhờ vậy đổi chính sách
không làm tính năng đang làm dở bị đổi yêu cầu (phản mục tiêu 2).

Dev muốn chuyển một tính năng sang phiên bản chính sách mới thì chạy `aisdlc risk reclassify`. Lệnh này cần console
thật, giống các lệnh duyệt. Nếu cổng Case đã duyệt thì lệnh mở lại cổng Case.

**Đổi mức tay:** `aisdlc risk set <level> --reason "…"`, hoặc nút trên web. Lệnh cần console thật. Nâng thì không bắt
buộc lý do; hạ thì bắt buộc. Lệnh chạy được khi đã có `risk.yaml` và cổng Case chưa duyệt (chưa nộp hoặc đang chờ
duyệt). Nếu cổng Case đang chờ duyệt thì đổi mức sẽ đưa cổng về trạng thái phải nộp lại.

### 3.6 Feature cũ

Tính năng đã qua cổng Thiết kế trước khi có tính năng này thì không có `risk.yaml`. Coi như mức `default` và không có
yêu cầu thêm. Hành vi giữ y như hiện tại.

## 4. Chính sách nằm ở đâu, ai sửa

### 4.1 Không bật web (`approvals: local` và chưa `aisdlc web login`)

- Nguồn duy nhất là `aisdlc/risk-policy.yaml` trong repo. Dev sửa file và đưa vào qua MR như mọi file khác.
- Số phiên bản dùng `version:` trong file, hoặc bằng 0 nếu không khai, cộng với hash nội dung.

### 4.2 Bật web **[Người dùng]**: quản lý trên web ở trang của repo, dev sửa và tự duyệt

- **Nguồn là web.** Bảng `risk_policy_versions(repo_id, version, body, hash, author, reason, source, created_at)`,
  thêm qua migration Flyway mới (Postgres). Dùng cùng khóa `repo_id` với các bảng `repos` và `feature_control`.
  Mức rủi ro và phiên bản của từng tính năng lấy từ sự kiện `RISK_CLASSIFIED`, ghi thêm thành cột của `features`.
  `/api/cli/policy` dùng cùng xác thực token và cùng khóa ký với `/api/cli/control`.
  `source` là một trong `import | web | preset-update`.
- Dev sửa trên web. Thay đổi nằm trong **bản nháp** (lưu phía client là đủ) cho đến khi bấm **Xuất bản**. Xuất bản đòi
  lý do và tạo phiên bản mới, có hiệu lực ngay. Không có bước duyệt. Ai đăng nhập web của repo cũng được sửa.
- **Đưa về máy dev:** web trả bản mới nhất trong phong bì ký Ed25519 qua `/api/cli/policy`, giống cơ chế
  `FeatureControl`. CLI lưu vào `.aisdlc/web/policy.json` ở các lần đồng bộ sẵn có (flush, `aisdlc status`) và khi chạy
  `aisdlc policy pull`. Hook và validator chỉ đọc cache, không gọi mạng.
- **Lần đầu repo kết nối web**: CLI gửi `aisdlc/risk-policy.yaml` lên. Web nhập thành phiên bản 1 (`source: import`)
  nếu repo đó chưa có phiên bản nào. Nếu đã có thì web không làm gì.
- Bật web rồi thì file `aisdlc/risk-policy.yaml` trong repo không còn là nguồn. `aisdlc status` nhắc khi file khác
  phiên bản web. Nút "Tải YAML" trên web giúp commit lại cho đồng bộ nếu dev muốn. **[Lựa chọn của Claude]**
- **CI** không cần chính sách: CI dùng `risk.yaml` của tính năng, và file này đã khóa theo hash cổng.

### 4.3 Mẫu (preset) **[Người dùng]**: init tự import từ AISDLC

- Mẫu nằm trong `aisdlc-core` (resource `risk-presets/<name>.yaml`), để cả CLI và web đọc được. Mỗi mẫu có `version`
  riêng, tăng khi AISDLC đổi mẫu.
- Đợt này có 2 mẫu **[Lựa chọn của Claude]**:
  - `generic`: không có rule nghiệp vụ. Chỉ có rule `label` cho các nhãn chung `money-movement`, `schema-change`,
    `auth`, `external-api` và yêu cầu theo mức.
  - `banking`: `generic` cộng thêm các rule hạch toán, phí, sổ cái, số dư.
- `aisdlc setup --preset <name>` chép mẫu vào `aisdlc/risk-policy.yaml`. Mặc định là `generic`. Chạy lại setup thì
  không ghi đè file đã có, giống các file khác của setup.
- **Mẫu mới:** web so `preset.version` của phiên bản đang hiệu lực với mẫu cùng tên đi kèm web. Mẫu đi kèm mới hơn thì
  trang Phiên bản hiện thông báo kèm diff theo từng mục: thêm/bớt rule, đổi yêu cầu, đổi tham số. Dev chọn áp dụng
  từng mục vào nháp, hoặc bỏ qua. Không bao giờ tự ghi đè. Khi không có web, `aisdlc policy diff-preset` in diff đó.

## 5. Kiểm tra và hiển thị

- **Cổng Case:** CasesValidator nhận thêm `requirements` từ `risk.yaml` và sinh `Finding` cho mỗi yêu cầu thiếu. Ví dụ:
  "Mức Cao yêu cầu case gửi trùng cho POST /fee-waivers; chưa có case nào có when.repeat ≥ 2". Finding chặn
  `submit cases` như các finding hiện có. Review card cổng Case có cờ đỏ cho từng mục thiếu.
- **Harness:** `aisdlc-junit` hỗ trợ `repeat`, `parallel`, `invariants` và `sql-zero`. **[Lựa chọn của Claude]**
  - `reset_sql`, `given.sql` và `given.stubs` chạy **một lần mỗi case**, không chạy lại cho từng request.
  - `repeat`: gửi tuần tự n lần.
  - `parallel`: n luồng chờ chung một hiệu lệnh (latch) rồi cùng gửi. Mỗi request dùng timeout sẵn có của HttpCaller.
    `then.db` kiểm sau khi cả n request xong. `statusCounts` chỉ đếm, không quan tâm thứ tự.
  - Bất biến và `sql-zero`: chạy sau mỗi case, kể cả case không chạm DB. Dùng cùng `DataSource` với `then.db`, trên
    kết nối read-only và rollback sau khi chạy. Vi phạm làm case đó FAIL, với thông điệp ghi tên bất biến và tối đa 5
    dòng vi phạm.
  - Ràng buộc "chỉ `SELECT`/`WITH`" do `RiskPolicy.parse` của core kiểm, nên áp dụng cả khi đọc file lẫn khi đọc
    trên web.
- **Eval:** nếu `risk.yaml` có `coverage`, kiểm coverage dòng mới thành kiểm tra **cứng** với ngưỡng `min`. Base để
  xác định "dòng mới" giữ như kiểm tra mềm hiện tại. Nếu không có, giữ cảnh báo mềm `coverageWarn` như cũ. `eval/report.md` có mục "Rủi ro" ghi mức, nguồn và trạng thái từng yêu
  cầu.
- **Review card và web:**
  - Thẻ Thiết kế hiện mức rủi ro và nguồn.
  - Thẻ Case hiện danh sách yêu cầu với trạng thái ĐẠT / THIẾU / SAU. "SAU" là yêu cầu kiểm ở Review.
  - Bảng tính năng có cột mức rủi ro.
- **Sự kiện:** `RISK_CLASSIFIED` và `RISK_OVERRIDDEN` vào luồng events sẵn có, để web hiển thị và có lịch sử.

## 6. Giao diện web

Theo prototype. Mọi trang nằm ở `/repos/<repo>/risk`. Có 5 tab, hash route:

1. **Rule nhận diện:**
   - bảng rule, mỗi rule có nhãn nguồn "mẫu <tên>" hoặc "dự án";
   - thêm, sửa (hộp thoại với loại, mức, mẫu khớp, ghi chú), bật/tắt;
   - khối "Nguồn phân loại khác" cho `classification.agent` và `classification.devLower`.
2. **Yêu cầu theo mức:** ma trận yêu cầu × mức. Tick để bật; ô số cho tham số. Nút "Yêu cầu tự viết" mở form tạo
   `sql-zero`.
3. **Thử phân loại:** nhập story, đường dẫn, bảng, endpoint, nhãn và mức agent. Thấy ngay mức, các rule đã khớp và
   yêu cầu tương ứng. Phép tính phải giống hệt lõi Java: web gọi endpoint server dùng chính code phân loại của
   `aisdlc-core`, không tính lại bằng JS.
4. **Phiên bản:** dòng thời gian các phiên bản, mỗi phiên bản ghi ai sửa, lý do, số tính năng đang gắn. Có thông báo
   mẫu mới kèm diff.
5. **Trên tính năng:** đặt ở trang tính năng sẵn có, không phải tab của trang chính sách. Hiện mức, nguồn, danh sách
   yêu cầu và nút Nâng/Hạ mức (hạ thì đòi lý do).

Thanh nháp dính đáy trang cho biết số thay đổi, kèm các nút "Bỏ nháp", "Xem thay đổi" và "Xuất bản". Nháp có thể
lưu trong `localStorage` của trình duyệt.

**Validate trước khi xuất bản:** dùng chính `RiskPolicy.parse` của core. Các lỗi:
- regex hỏng;
- glob rỗng;
- mức không có trong thang;
- id rule trùng;
- yêu cầu không có trong thư viện hoặc thiếu tham số bắt buộc;
- `sql-zero` không bắt đầu bằng `SELECT` hoặc `WITH`.

Xuất bản mà làm **nới** yêu cầu thì hộp thoại xác nhận nêu rõ phần bị nới. Nới gồm: tắt yêu cầu, giảm tham số, tắt
rule hoặc hạ mức của rule. Vẫn cho xuất bản vì dev tự duyệt; mục đích là để dev thấy rõ mình đang nới gì.

## 7. Bảo vệ

- Hook thêm `aisdlc/features/*/risk.yaml` và `.aisdlc/web/policy.json` vào danh sách file trạng thái chặn ghi.
- Khi bật web, hook cũng chặn agent ghi `aisdlc/risk-policy.yaml`, vì đó là chính sách. Khi không có web, file đó đổi
  qua MR. Agent cũng không được sửa nó trong tính năng: hook chặn như file trạng thái, trừ khi dev cho phép theo đúng
  cơ chế cho phép file của bước.
- `aisdlc risk set` và `aisdlc risk reclassify` cần console thật, giống `approve`.
- Đánh giá của agent chỉ nâng được mức (`raise`). Lõi bỏ qua nếu chính sách đặt `agent: off`.

## 8. Kiểm thử

- **Core:**
  - parse và validate chính sách, gồm các lỗi ở §6;
  - phân loại: mỗi kind khớp và không khớp, nhiều rule, agent nâng, agent bị tắt, dev hạ có và không có lý do;
  - so khớp glob với glob cho `path`;
  - CasesValidator với từng yêu cầu thiếu và đủ;
  - feature không có `risk.yaml` giữ hành vi cũ;
  - **không có chuỗi nghiệp vụ trong code lõi:** test quét `aisdlc-core/src/main/java` để chắc không có
    "hạch toán", "ledger", "fee".
- **Harness:** `repeat` (DB kiểm sau lần cuối), `parallel` với `statusCounts`, bất biến fail đúng case, `sql-zero` fail
  với thông điệp có tên.
- **CLI:**
  - `setup --preset` tạo file và không ghi đè khi chạy lại;
  - `submit design` ghi `risk.yaml`;
  - hook chặn ghi `risk.yaml`;
  - `risk set` hạ mức thiếu lý do thì exit ≠ 0;
  - `policy pull` dùng cache ký và từ chối chữ ký sai.
- **Web:**
  - nhập lần đầu tạo v1, lần sau không;
  - xuất bản tạo phiên bản mới;
  - payload ký đúng;
  - endpoint thử phân loại trả kết quả giống core;
  - diff mẫu mới.
- **E2E:** thêm một biến thể của `PAY-1`, nơi chính sách của fixture đặt rule làm PAY-1 thành mức Cao:
  - submit cases thiếu case gửi trùng thì bị chặn;
  - thêm case thì qua;
  - eval có mục Rủi ro.

  Chạy ở cả `WEB=0` (chính sách từ file) và `WEB=1` (chính sách từ web).

## 9. Thứ tự làm **[Lựa chọn của Claude]**

1. Core: schema chính sách, mẫu, phân loại, `risk.yaml`, yêu cầu trong CasesValidator, eval coverage cứng.
2. Harness: `repeat`, `parallel`, `invariants`, `sql-zero`.
3. CLI và plugin: `setup --preset`, `policy show/pull/diff-preset`, `risk set/reclassify`, ghi `risk.yaml` lúc submit,
   hook, template `/aisdlc:design` và `cases`.
4. Web: bảng phiên bản, nhập lần đầu, API ký, trang 5 tab, cột mức trên bảng tính năng.
5. E2E và tài liệu (`docs/README.vi.md` thêm mục chính sách rủi ro, README gốc).

Bước 1–3 dùng được khi không có web. Bước 4 thêm phần quản lý trên web.

## 10. Để builder quyết

- Cách lưu nháp phía client: `localStorage` hoặc state trong trang.
- Câu chữ của thông điệp finding, miễn là nêu được yêu cầu, chỗ thiếu và cách sửa.
- Cách so khớp glob với glob chính xác đến đâu (§3.2), miễn là có test cho các trường hợp `**/x/**` với `src/**/x/*.java`.
- Chia nhỏ endpoint web và component UI.
