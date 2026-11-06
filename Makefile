.PHONY: build test test-short vet fmt-check check plugin-dist

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
