.PHONY: build test lint fmt vet clean install demo help

BINARY := cc360
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

## build: Compile binary
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## test: Run tests with race detector
test:
	go test ./... -race

## cover: Run tests with coverage report
cover:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out
	@rm -f coverage.out

## lint: Run golangci-lint
lint:
	golangci-lint run

## vet: Run go vet
vet:
	go vet ./...

## fmt: Check formatting (fails if files need formatting)
fmt:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)

## install: Install to GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" .

## demo: Regenerate demo.gif via VHS
demo: build
	@command -v vhs >/dev/null 2>&1 || { echo "vhs not installed: go install github.com/charmbracelet/vhs@latest"; exit 1; }
	PATH="$(CURDIR):$$PATH" vhs demo.tape

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
