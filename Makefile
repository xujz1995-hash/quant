GOCACHE ?= /tmp/go-cache
GOPATH ?= /tmp/go

.PHONY: tidy run build fmt

tidy:
	GOCACHE=$(GOCACHE) GOPATH=$(GOPATH) go mod tidy

fmt:
	gofmt -w $(shell rg --files -g '*.go')

build:
	GOCACHE=$(GOCACHE) GOPATH=$(GOPATH) go build ./...

run:
	GOCACHE=$(GOCACHE) GOPATH=$(GOPATH) go run .
