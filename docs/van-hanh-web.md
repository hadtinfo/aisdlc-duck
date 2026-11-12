# Vận hành server web AISDLC

> English: [web-operations.md](web-operations.md).

Dành cho người triển khai `aisdlc-web`. Cách dùng web của dev: mục 6 trong [huong-dan-su-dung.md](huong-dan-su-dung.md).

## 1. Thành phần

- Binary `aisdlc-web` (`cmd/aisdlc-web`): server HTTP, UI React nhúng, migration goose nhúng. Không đối số thì chạy server; `aisdlc-web user ...` là quản trị user (không mở cổng, chỉ cần DB).
- `Dockerfile.web` ở gốc repo: dựng UI (node), build binary tĩnh (`CGO_ENABLED=0`), chạy trên distroless (`nonroot`, uid 65532), cổng 8080, volume `/data`.
- PostgreSQL (bản 16 đã thử).

```bash
docker build -f Dockerfile.web -t aisdlc-web:<phiên bản> .
```

Muốn chạy không qua Docker: `make build-web` (dựng UI rồi build `bin/aisdlc-web`).

## 2. Biến môi trường

| Biến | Mặc định | Ý nghĩa |
| --- | --- | --- |
| `AISDLC_LISTEN` | `:8080` | Địa chỉ nghe. Chỉ nghe localhost: `127.0.0.1:8080` |
| `AISDLC_DB_URL` | `postgres://localhost:5432/aisdlc` | URL Postgres dạng pgx (xem mục 4 về `pool_max_conns`) |
| `AISDLC_DB_USER` | rỗng | Ghi đè user trong URL. URL không có user mà biến này cũng rỗng thì dùng `aisdlc` |
| `AISDLC_DB_PASSWORD` | rỗng | Ghi đè mật khẩu trong URL. Đặt qua secret, không ghi vào URL |
| `AISDLC_SIGNING_KEY_FILE` | `/data/signing-key` | File khóa ký Ed25519 (PEM). Chưa có thì tự sinh (quyền 0600) |
| `AISDLC_SIGNING_PREVIOUS_KEY_FILE` | rỗng | Khóa ký cũ khi xoay khóa: server ký xác nhận khóa mới bằng khóa cũ để CLI đã ghim tự chuyển |
| `TZ` | `UTC` | Múi giờ chạy purge hằng ngày (mục 5) |
| `AISDLC_LOCALE` | `vi` | Ngôn ngữ cho người vận hành (`vi` hoặc `en`): log của server, lỗi khởi động và lệnh `aisdlc-web user ...`. Không ảnh hưởng tới người dùng: mỗi request theo lựa chọn của người dùng đã đăng nhập, rồi `Accept-Language`, rồi `vi` |

Hai biến `AISDLC_WEB_URL` và `AISDLC_WEB_TOKEN` là của CLI/CI, không phải của server.

## 3. Khóa ký dùng chung giữa các replica

Server ký cache trạng thái và lệnh điều khiển bằng khóa Ed25519; CLI của dev ghim khóa công khai lúc `aisdlc web login`. Vì vậy **mọi replica phải dùng cùng một khóa**:

- Server tự sinh khóa ở lần khởi động đầu nếu file chưa có. Vì vậy phải **mount secret chứa khóa dùng chung TRƯỚC lần khởi động đầu tiên** của bất kỳ replica nào (tạo trước một cặp PEM PKCS#8, hoặc lấy khóa của replica đầu tiên đưa vào secret), mount (Kubernetes Secret, Docker secret) tại `AISDLC_SIGNING_KEY_FILE`, chỉ đọc. Không để mỗi replica tự sinh khóa riêng: CLI sẽ thấy khóa khác nhau tùy replica và từ chối cache.
- Volume `/data` mặc định chỉ phù hợp một replica. Nhiều replica thì mount secret thay volume.
- File có quyền rộng hơn 0600 thì server chỉ cảnh báo trong log; file hỏng hoặc không đọc được thì server từ chối khởi động, không ghi đè.
- Sao lưu khóa. Mất khóa thì mọi dev phải `aisdlc web login` lại.
- Xoay khóa: mount khóa mới ở `AISDLC_SIGNING_KEY_FILE` và khóa cũ ở `AISDLC_SIGNING_PREVIOUS_KEY_FILE`; giữ tới khi mọi CLI đã đồng bộ.

## 4. Cơ sở dữ liệu

`AISDLC_DB_URL` là URL pgx, tham số pool đặt ngay trong URL:

```
postgres://aisdlc-db:5432/aisdlc?sslmode=require&pool_max_conns=20&pool_min_conns=2
```

- `pool_max_conns`: số kết nối tối đa của **mỗi replica** (mặc định của pgx: số CPU, tối thiểu 4). Tổng `replica × pool_max_conns` phải nhỏ hơn `max_connections` của Postgres (chừa chỗ cho admin và migration).
- Mật khẩu để ở `AISDLC_DB_PASSWORD` (từ secret), không viết vào URL.
- **Migration chạy tự động** lúc khởi động (goose, nhúng trong binary), có khóa phiên Postgres (advisory lock): nhiều replica khởi động cùng lúc thì lần lượt migrate, không đua nhau. Không cần bước migrate riêng. Migration lỗi thì server không lên và log nêu tên file lỗi.
- Lệnh `aisdlc-web user ...` cũng migrate trước khi chạy.
- Migration `00002_user_locale.sql` thêm cột `users.locale` cho phép `NULL` (`vi` hoặc `en`; `NULL` = chưa chọn), lưu ngôn ngữ giao diện mỗi người chọn ở bộ chọn EN/VI (`PUT /api/me/locale`). User đã có giữ `NULL` và thấy giao diện theo ngôn ngữ đã chọn lần trước trên trình duyệt đó, không có thì theo ngôn ngữ trình duyệt, cho tới khi tự chọn.

## 5. Dọn dữ liệu cũ (purge)

Mỗi ngày lúc **03:00 giờ địa phương** của tiến trình, server xóa sự kiện cũ. Giờ địa phương theo `TZ` (ví dụ `TZ=Asia/Ho_Chi_Minh`). Binary nhúng sẵn cơ sở dữ liệu múi giờ (`time/tzdata`) nên `TZ` có tác dụng cả trên ảnh distroless không có `/usr/share/zoneinfo`. Không đặt `TZ` thì purge chạy lúc 03:00 UTC. Mỗi replica tự chạy purge; lệnh xóa idempotent nên chạy trùng không hại.

## 6. TLS, proxy và cookie

Server nói HTTP thường. Cookie phiên có `HttpOnly`, `SameSite=Strict`, và chỉ có `Secure` **khi TLS kết thúc ngay tại tiến trình** (request đến có TLS). Server không đọc `X-Forwarded-Proto`. Hệ quả khi đặt sau reverse proxy/ingress kết thúc TLS: kết nối proxy → server là HTTP thường nên cookie **không** có `Secure`. Biện pháp:

- Chỉ cho phép truy cập từ ngoài qua HTTPS ở proxy (chuyển hướng HTTP sang HTTPS, HSTS).
- Nếu proxy hỗ trợ, thêm thuộc tính `Secure` cho cookie `Set-Cookie` ở proxy.
- Không mở cổng 8080 ra ngoài proxy (`AISDLC_LISTEN=127.0.0.1:8080` hoặc NetworkPolicy).

## 7. Triển khai mẫu

```bash
docker run -d --name aisdlc-web -p 127.0.0.1:8080:8080 \
  -e TZ=Asia/Ho_Chi_Minh \
  -e AISDLC_DB_URL='postgres://aisdlc-db:5432/aisdlc?pool_max_conns=20' \
  -e AISDLC_DB_PASSWORD="$(cat /run/secrets/db-password)" \
  -e AISDLC_SIGNING_KEY_FILE=/secrets/signing-key \
  -v /srv/aisdlc/signing-key:/secrets/signing-key:ro \
  aisdlc-web:<phiên bản>
```

(`docker run` chỉ để thử một replica; trên Kubernetes dùng Deployment, Secret cho `signing-key` và mật khẩu DB. Manifest không thuộc repo này.)

Kiểm: mở `/` thấy trang đăng nhập; log không có dòng lỗi migration; `docker exec aisdlc-web /aisdlc-web user list` in được danh sách user.

## 8. Quản trị user

Dùng binary trong container (distroless không có shell, gọi thẳng binary):

```bash
docker exec aisdlc-web /aisdlc-web user add dev "Tên Dev" --email dev@example.com   # in token login một lần
docker exec aisdlc-web /aisdlc-web user role dev lead                                # dev | lead
docker exec aisdlc-web /aisdlc-web user login-token dev                              # cấp thêm token login
docker exec aisdlc-web /aisdlc-web user ci-token gitlab                              # token CI, in một lần
docker exec aisdlc-web /aisdlc-web user list
```

Token chỉ hiện đúng một lần, lưu SHA-256 trong DB; nhật ký audit không chứa token. Lộ token thì thu hồi ở trang `/me`.

## 9. Kết nối từ máy dev và CI

- Dev: `aisdlc web login` (URL web + token ingest), `aisdlc web status`, `aisdlc web push`, `aisdlc web approve-token`, `aisdlc web migrate`, `aisdlc policy pull`. Xem mục 6 của hướng dẫn sử dụng.
- CI: biến `AISDLC_WEB_URL` và `AISDLC_CI_TOKEN` (Masked, Protected).

## 10. Sự cố thường gặp

| Triệu chứng | Nguyên nhân | Cách xử lý |
| --- | --- | --- |
| Server thoát ngay, "Không nạp được khóa ký …" | File khóa hỏng, hoặc sai đường dẫn secret | Kiểm file; không xóa khóa đang dùng |
| "không kết nối được Postgres" | URL/mật khẩu sai, Postgres chưa sẵn sàng | Kiểm `AISDLC_DB_URL`, `AISDLC_DB_PASSWORD`, mạng |
| "migration … lỗi" | Migration hỏng hoặc DB lệch phiên bản | Đọc log, sửa DB; không chạy tay phần đã áp |
| `too many clients` từ Postgres | `replica × pool_max_conns` vượt `max_connections` | Hạ `pool_max_conns` hoặc tăng `max_connections` |
| CLI báo khóa ký đổi | Replica khác dùng khóa khác | Mount cùng một secret cho mọi replica (mục 3) |
| Đăng nhập xong cookie không giữ | Truy cập qua HTTP thay vì HTTPS ở proxy | Dùng HTTPS ở proxy (mục 6) |
| Purge chạy sai giờ | Chưa đặt `TZ` | Đặt `TZ=Asia/Ho_Chi_Minh` |
