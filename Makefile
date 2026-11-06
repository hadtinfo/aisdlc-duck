.PHONY: e2e e2e-web ci build-web-bin build test test-short vet fmt-check check plugin-dist ui ui-test build-web

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Hai binary: aisdlc (CLI và hook, khởi động nhanh) và aisdlc-harness (harness black-box, testcontainers) mà aisdlc
# gọi như tiến trình con; aisdlc tìm aisdlc-harness cạnh nó (không có thì PATH; $$AISDLC_HARNESS chỉ để test/dev, bị bỏ qua trong Claude Code).
build:
	go build -o bin/aisdlc ./cmd/aisdlc && go build -o bin/aisdlc-harness ./cmd/aisdlc-harness

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
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="-s -w" -o plugin/lib/aisdlc-harness-$$os-$$arch ./cmd/aisdlc-harness || exit 1; \
	done

# UI React: build rồi chép vào thư mục nhúng của server (dist chỉ có .gitkeep được commit; chưa build UI thì server phục vụ trang placeholder).
ui:
	cd web/ui && npm ci && npm run build && find ../../internal/web/spa/dist -mindepth 1 ! -name .gitkeep -delete && cp -r dist/. ../../internal/web/spa/dist/

ui-test:
	cd web/ui && npm ci && npm run test -- --run

# Chỉ build binary, dùng khi dist UI đã có sẵn (artifact của job ui trong CI).
build-web-bin:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/aisdlc-web ./cmd/aisdlc-web

build-web:
	$(MAKE) ui && $(MAKE) build-web-bin

# Chạy cục bộ những gì CI chạy ở các stage check và ui.
ci:
	$(MAKE) check && $(MAKE) ui && $(MAKE) ui-test

# E2E toàn bộ (cần Go, Docker, git, jq, curl, script, Node + npm): repo đầu (PAY-1 FULL, FIX-2 LITE, âm tính), web
# (server Go + Postgres docker run, Playwright), flow, session. Log ở e2e/out/. Chạy lại liền nhiều lần được.
e2e:
	bash e2e/run-e2e.sh

# Chỉ phần web: repo local/web-service (duyệt trên web, điều khiển agent) và flow-service ở chế độ web.
e2e-web:
	E2E_PARTS=web,flow WEB=1 bash e2e/run-e2e.sh
