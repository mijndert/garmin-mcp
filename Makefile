BINARY  := garmin-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test lint vet fmt clean run-http

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -trimpath -ldflags "$(LDFLAGS)" .

test:
	go test -race -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint: vet
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed; skipping"

clean:
	rm -rf $(BINARY) dist

# Handy for testing with MCP Inspector: npx @modelcontextprotocol/inspector
run-http: build
	./$(BINARY) --http 127.0.0.1:8787 --log-level debug
