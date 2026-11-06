.PHONY: build test test-short vet fmt-check check plugin-dist ui ui-test build-web

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

build:
	go build -o bin/aisdlc ./cmd/aisdlc

# Số gói test chạy song song. Mặc định của Go (= số CPU) mở hàng chục container Docker cùng lúc (mỗi gói web một
# Postgres, harness thêm Kafka) với một Ryuk dùng chung: máy bận thì test Docker chập chờn ("wait for reaper …:
# context deadline exceeded") và số đo độ trễ hook lệch. Đổi bằng `make test TEST_P=8`. Thời hạn Ryuk được nới
# trong harnesstest.RequireDocker (TuneTestcontainers).
TEST_P ?= 4

test:
	go test -p $(TEST_P) ./...

test-short:
	AISDLC_SKIP_DOCKER=1 go test ./...

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt cần chạy cho:"; gofmt -l .; exit 1; }

check: fmt-check vet test

plugin-dist:
	@mkdir -p plugin/lib
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="-s -w" -o plugin/lib/aisdlc-$$os-$$arch ./cmd/aisdlc || exit 1; \
	done

# UI React: build rồi chép vào thư mục nhúng của server (dist chỉ có .gitkeep được commit; chưa build UI thì server phục vụ trang placeholder).
ui:
	cd web/ui && npm ci && npm run build && find ../../internal/web/spa/dist -mindepth 1 ! -name .gitkeep -delete && cp -r dist/. ../../internal/web/spa/dist/

ui-test:
	cd web/ui && npm ci && npm run test -- --run

build-web: ui
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/aisdlc-web ./cmd/aisdlc-web
