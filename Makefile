# Makefile for go-mrbc.

GO ?= go
BINARY ?= gomrbc

.PHONY: build test conformance fmt vet clean

build:
	$(GO) build -o $(BINARY) ./cmd/gomrbc

test:
	$(GO) test ./...

conformance:
	$(GO) test -v ./mrbc -run TestConformance

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './mruby/*')

vet:
	$(GO) vet ./...

clean:
	rm -f $(BINARY)
