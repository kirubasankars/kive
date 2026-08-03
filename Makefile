# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

# Kive — public build helpers.
# SQLite requires CGO (see public-docs/reference/cli.md).

CGO_ENABLED ?= 1
export CGO_ENABLED

BINARY   ?= kive
GIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
VERSION  ?= $(shell git describe --tags --exact-match 2>/dev/null || echo dev)
BUILD_LDFLAGS ?= -X kive/buildinfo.GitHash=$(GIT_HASH) -X kive/buildinfo.Version=$(VERSION)

.PHONY: all build clean help

all: build

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  build   Build $(BINARY) (CGO_ENABLED=$(CGO_ENABLED))"
	@echo "  clean   Remove $(BINARY)"
	@echo ""
	@echo "Variables: BINARY, CGO_ENABLED, VERSION, GIT_HASH"

build:
	CGO_ENABLED=1 go build -ldflags "$(BUILD_LDFLAGS)" -o $(BINARY) .

clean:
	rm -f $(BINARY)
