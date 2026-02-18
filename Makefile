# Copyright 2026 matter-cli contributors
# SPDX-License-Identifier: Apache-2.0

BINARY := matter
MODULE := github.com/p0fi/matter-cli
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X $(MODULE)/cli.version=$(VERSION) -X $(MODULE)/cli.commit=$(COMMIT) -X $(MODULE)/cli.date=$(DATE)"

.PHONY: build test lint clean fmt vet

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/matter/

test:
	go test ./... -race -count=1

test-cover:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ coverage.out coverage.html
