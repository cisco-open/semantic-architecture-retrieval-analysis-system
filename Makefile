# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 Cisco Systems, Inc. and its affiliates
#
# See CONTRIBUTORS.md for full contributor list.

.PHONY: build install test lint clean release

BINARY    := saras
MODULE    := github.com/cisco-open/semantic-architecture-retrieval-analysis-system
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS   := -ldflags "-s -w -X $(MODULE)/internal/cli.version=$(VERSION)"
BUILD_DIR := bin

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/saras

install:
	go install $(LDFLAGS) ./cmd/saras

test:
	go test ./... -count=1 -timeout 120s

test-verbose:
	go test ./... -v -count=1 -timeout 120s

test-coverage:
	go test ./... -coverprofile=coverage.out -timeout 120s
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

vet:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

release:
	goreleaser release --clean

release-snapshot:
	goreleaser release --snapshot --clean

all: fmt vet lint test build
