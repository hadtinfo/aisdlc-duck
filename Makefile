.PHONY: build test test-short vet fmt-check check plugin-dist ui ui-test build-web

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

# UI React: build rồi chép vào thư mục nhúng của server (placeholder được commit sẵn để go build chạy khi chưa build UI).
ui:
	cd web/ui && npm ci && npm run build && rm -rf ../../internal/web/spa/dist && cp -r dist ../../internal/web/spa/dist
	@git update-index --skip-worktree internal/web/spa/dist/index.html 2>/dev/null || true  # placeholder đã commit bị bản build ghi đè: đừng hiện là sửa đổi

ui-test:
	cd web/ui && npm ci && npm run test -- --run

build-web: ui
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/aisdlc-web ./cmd/aisdlc-web
