VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test dist clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o zenavo-mcp ./cmd/zenavo-mcp

test:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -race ./...

# Cross-compiled binaries and a SHA256SUMS file in dist/.
dist:
	rm -rf dist && mkdir -p dist
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/zenavo-mcp_$${os}_$${arch}$$ext ./cmd/zenavo-mcp || exit 1; \
	done
	cd dist && shasum -a 256 zenavo-mcp_* > SHA256SUMS

clean:
	rm -rf zenavo-mcp dist
