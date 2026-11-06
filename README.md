# AISDLC (Go)

AISDLC để agent Claude Code viết tài liệu, case và code theo quy trình **BRD → Thiết kế → Case → Code từng bước → Review**, còn dev duyệt từng cổng. Hook của plugin chặn agent đi tắt; CI chấm lại mỗi MR. Bản Go port từ bản Java, dùng cho dịch vụ Go có migration goose và PostgreSQL.

## Bắt đầu nhanh

Cần Go, Docker, git và Claude Code.

```bash
git clone <repo aisdlc> ~/aisdlc && ~/aisdlc/plugin/install.sh   # build binary vào plugin/lib, symlink ~/.local/bin/aisdlc
```

Trong Claude Code: `/plugin marketplace add ~/aisdlc` rồi `/plugin install aisdlc@aisdlc`. Ở gốc repo dịch vụ: `aisdlc setup`. Sau đó `/aisdlc:init <KEY>` để bắt đầu một tính năng.

## Tài liệu

- [docs/huong-dan-su-dung.md](docs/huong-dan-su-dung.md): hướng dẫn cho dev (cài đặt, luồng FULL/LITE, viết case, eval và CI, web, giới hạn đã biết).
- [docs/van-hanh-web.md](docs/van-hanh-web.md): triển khai và vận hành server web.
- `docs/legacy-java/`: tài liệu bản Java cũ.
- Spec port: `docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md`.

## Phát triển

| Lệnh | Việc |
| --- | --- |
| `make build` | Build `bin/aisdlc` |
| `make check` | gofmt, vet, test |
| `make test-short` | Test bỏ qua phần cần Docker (`AISDLC_SKIP_DOCKER=1`) |
| `make plugin-dist` | Build binary linux/darwin, amd64/arm64 vào `plugin/lib/` |
| `make build-web` | Dựng UI rồi build `bin/aisdlc-web` |
| `bash docs/docs-check.sh` | Chạy các khối lệnh đánh dấu `# chạy-được` trong hướng dẫn trên repo tạm |
