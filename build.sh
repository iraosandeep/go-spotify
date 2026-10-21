#!/usr/bin/env sh
# Build gospotify for every supported platform into dist/.
set -eu

VERSION=$(cat VERSION | tr -d ' \r\n')
OUT=${OUT:-dist}
LDFLAGS="-s -w"

# Bundle the web UI (embedded into every binary)
(cd web && bun install --frozen-lockfile >/dev/null && bun run build >/dev/null)

TARGETS="
linux/amd64 linux/arm64 linux/arm
darwin/amd64 darwin/arm64
windows/amd64 windows/arm64
freebsd/amd64
"

rm -rf "$OUT"
mkdir -p "$OUT"

for t in $TARGETS; do
  os=${t%/*}
  arch=${t#*/}
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  name="gospotify-$VERSION-$os-$arch$ext"
  printf '  %-34s' "$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$OUT/$name" .
  printf '%s\n' "$(du -h "$OUT/$name" | cut -f1)"
done

(cd "$OUT" && (command -v sha256sum >/dev/null && sha256sum gospotify-* || shasum -a 256 gospotify-*) > checksums.txt)
echo "built $(ls -1 "$OUT" | grep -c '^gospotify-') binaries in $OUT/"
