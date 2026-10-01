GO ?= go

.PHONY: all build test lint fmt
all: lint test build

build:
	$(GO) build -o bin/sourced-lab ./cmd/sourced-lab

test:
	$(GO) test -race ./...

# gofmt and go vet. staticcheck isn't downloaded yet: ask before adding it.
lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	$(GO) vet ./...

fmt:
	gofmt -w .

# Run every experiment config (needs the corpus: see README).
bench: build
	@for f in bench/experiments/*.yaml; do bin/sourced-lab bench run $$f || exit 1; done
