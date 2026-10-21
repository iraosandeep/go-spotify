.PHONY: all build web test dev clean

LDFLAGS := -s -w

all: build

web:
	@echo "Bundling web UI..."
	@cd web && bun install >/dev/null && bun run build

build: web
	go build -trimpath -ldflags="$(LDFLAGS)" -o gospotify .
	@echo "Built ./gospotify ($$(du -h gospotify | cut -f1))"

dev: web
	go run . -dev web/dist

test: web
	go test ./...

clean:
	rm -f gospotify
