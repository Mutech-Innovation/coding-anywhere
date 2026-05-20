.PHONY: help install dev build test lint clean fmt go-build go-test go-lint

help:
	@echo "Coding Anywhere — common dev tasks"
	@echo ""
	@echo "  make install      Install JS deps (pnpm) and tidy Go modules"
	@echo "  make dev          Run web + daemon + relay together (turbo)"
	@echo "  make build        Build all artefacts"
	@echo "  make test         Run all tests (Go + JS)"
	@echo "  make lint         Lint everything"
	@echo "  make fmt          Format JS/TS (Prettier) and Go (gofmt)"
	@echo "  make clean        Remove node_modules / build output"

install:
	pnpm install
	go mod tidy

dev:
	pnpm dev

build:
	pnpm build

test:
	pnpm test
	go test ./...

lint:
	pnpm lint
	go vet ./...

fmt:
	pnpm format
	gofmt -w apps/daemon apps/relay internal

clean:
	pnpm clean
	rm -rf bin
