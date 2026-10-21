.PHONY: all build web test dist publish dev clean help

VERSION ?= $(shell cat VERSION 2>/dev/null | tr -d ' \r\n')
# Support 'make publish 0.2.0' where the version is passed as the next goal
VERSION_ARG := $(filter-out publish,$(MAKECMDGOALS))
ifeq ($(strip $(VERSION_ARG)),)
  TARGET_VERSION := $(VERSION)
else
  TARGET_VERSION := $(strip $(VERSION_ARG))
  %:
	@:
endif
CLEAN_VERSION := $(patsubst v%,%,$(TARGET_VERSION))

LDFLAGS := -s -w

all: build

help:
	@echo "gospotify make targets:"
	@echo "  make build             - build ./gospotify for this platform (UI embedded)"
	@echo "  make web               - bundle the React UI into web/dist"
	@echo "  make dev               - run with the UI served from web/dist on disk"
	@echo "  make test              - run the Go test suite"
	@echo "  make dist              - cross-compile binaries into dist/"
	@echo "  make publish <version> - bump VERSION, commit, tag and build dist binaries"
	@echo "  make clean             - remove build artifacts"

web:
	@echo "Bundling web UI..."
	@cd web && bun install --frozen-lockfile >/dev/null && bun run build

build: web
	go build -trimpath -ldflags="$(LDFLAGS)" -o gospotify .
	@echo "Built ./gospotify ($$(du -h gospotify | cut -f1))"

dev: web
	go run . -dev web/dist

test: web
	go test ./...

dist: web
	@./build.sh

publish:
	@if [ -z "$(CLEAN_VERSION)" ]; then \
		echo "Usage: make publish <version> (e.g. make publish 0.2.0)"; exit 1; \
	fi
	@echo "==> Preparing release v$(CLEAN_VERSION) (previous: $$(cat VERSION))"
	@echo "$(CLEAN_VERSION)" > VERSION
	@./build.sh
	@git add VERSION web/dist
	@git commit -m "Release v$(CLEAN_VERSION)" || true
	@git tag -fa "v$(CLEAN_VERSION)" -m "Release v$(CLEAN_VERSION)"
	@echo ""
	@echo "Tag v$(CLEAN_VERSION) created. Publish with:"
	@echo "  git push origin main --tags"

clean:
	rm -f gospotify
	rm -rf dist/
