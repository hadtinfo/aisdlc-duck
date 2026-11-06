.PHONY: e2e e2e-web ci build-web-bin build test test-short vet fmt-check check plugin-dist ui ui-test build-web

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

build:
	go build -o bin/aisdlc ./cmd/aisdlc

test:
	go test ./...

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

# Chỉ build binary, dùng khi dist UI đã có sẵn (artifact của job ui trong CI).
build-web-bin:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/aisdlc-web ./cmd/aisdlc-web

build-web:
	$(MAKE) ui && $(MAKE) build-web-bin

# Chạy cục bộ những gì CI chạy ở các stage check và ui.
ci:
	$(MAKE) check && $(MAKE) ui && $(MAKE) ui-test

# Placeholder: Task 4 thay bằng e2e/run-e2e.sh. Cố ý fail để job CI không xanh giả.
e2e:
	@echo "e2e chưa có: Task 4 sẽ thêm e2e/run-e2e.sh" >&2; exit 1

e2e-web:
	@echo "e2e-web chưa có: Task 4 sẽ thêm e2e/run-e2e.sh" >&2; exit 1
