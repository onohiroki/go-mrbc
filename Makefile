# Makefile for go-mrbc.

GO ?= go
BINARY ?= gomrbc

.PHONY: build test conformance compat fmt vet clean

build:
	$(GO) build -o $(BINARY) ./cmd/gomrbc

test:
	$(GO) test ./...

conformance:
	$(GO) test -v ./mrbc -run TestConformance

compat:
	$(GO) run ./cmd/compat mruby/test mruby/mrbgems

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './mruby/*')

vet:
	$(GO) vet ./...

clean:
	rm -f $(BINARY)
