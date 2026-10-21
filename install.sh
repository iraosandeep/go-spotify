#!/usr/bin/env sh
# Installer for gospotify — a terminal-styled Spotify player in your browser.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/iraosandeep/go-spotify/main/install.sh | sh
#
# Environment variables:
#   VERSION         - version to install (e.g. "0.1.0", default: "latest")
#   INSTALL_DIR     - target directory (default: first writable of ~/.local/bin, ~/bin, /usr/local/bin)
#   GOSPOTIFY_REPO    - GitHub repository (default: iraosandeep/go-spotify)

set -eu

REPO="${GOSPOTIFY_REPO:-iraosandeep/go-spotify}"
VERSION="${VERSION:-latest}"

BOLD="\033[1m"
GREEN="\033[38;5;108m"
AMBER="\033[38;5;179m"
RED="\033[38;5;167m"
RESET="\033[0m"

log_info() { printf " ${GREEN}✓${RESET} %b\n" "$1"; }
log_step() { printf " ${AMBER}›${RESET} %b\n" "$1"; }
log_warn() { printf " ${AMBER}!${RESET} %b\n" "$1"; }
log_error() { printf " ${RED}✗${RESET} %b\n" "$1" >&2; }

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux*) OS="linux" ;;
  darwin*) OS="darwin" ;;
  freebsd*) OS="freebsd" ;;
  msys* | cygwin* | mingw*) OS="windows" ;;
  *) log_error "Unsupported operating system: $OS"; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64 | amd64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  armv7* | armv6* | arm) ARCH="arm" ;;
  *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
esac

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 30 --retry 2 "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- -T 15 -t 3 "$1"
  else
    log_error "Neither curl nor wget found in PATH."; exit 1
  fi
}

fetch_file() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 300 --retry 2 -o "$2" "$1"
  else
    wget -q -T 30 -t 3 -O "$2" "$1"
  fi
}

log_step "Finding the latest release of ${BOLD}${REPO}${RESET} (${OS}/${ARCH})..."
if [ "$VERSION" = "latest" ]; then
  VERSION=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
    grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"v?([^"]+)".*/\1/' || true)
  if [ -z "$VERSION" ]; then
    VERSION=$(fetch "https://raw.githubusercontent.com/${REPO}/main/VERSION" 2>/dev/null | tr -d ' \r\n' || true)
  fi
fi
if [ -z "$VERSION" ]; then
  log_error "Could not determine the version. Check your network or set VERSION=x.y.z."
  exit 1
fi

EXT=""
[ "$OS" = "windows" ] && EXT=".exe"

is_writable() {
  [ -n "$1" ] && mkdir -p "$1" 2>/dev/null && [ -w "$1" ]
}

TARGET_DIR=""
USE_SUDO=0
if [ -n "${INSTALL_DIR:-}" ] && is_writable "$INSTALL_DIR"; then
  TARGET_DIR="$INSTALL_DIR"
fi
# Prefer a writable directory that is already on PATH
if [ -z "$TARGET_DIR" ]; then
  for d in "${HOME:-}/.local/bin" "${HOME:-}/bin" "/usr/local/bin"; do
    case ":${PATH:-}:" in
      *":$d:"*) if is_writable "$d"; then TARGET_DIR="$d"; break; fi ;;
    esac
  done
fi
if [ -z "$TARGET_DIR" ]; then
  if is_writable "${HOME}/.local/bin"; then
    TARGET_DIR="${HOME}/.local/bin"
  elif command -v sudo >/dev/null 2>&1 && [ -d /usr/local/bin ]; then
    TARGET_DIR="/usr/local/bin"
    USE_SUDO=1
  else
    log_error "No writable install directory found; set INSTALL_DIR."; exit 1
  fi
fi
TARGET_BIN="${TARGET_DIR}/gospotify${EXT}"

BINARY="gospotify-${VERSION}-${OS}-${ARCH}${EXT}"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY}"
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t gospotify)"
trap 'rm -rf "$TMP_DIR"' EXIT

log_step "Downloading ${BINARY}..."
if ! fetch_file "$URL" "$TMP_DIR/gospotify" 2>/dev/null; then
  log_error "Download failed: $URL"
  log_warn "If v${VERSION} was just tagged, the release build may still be running."
  exit 1
fi
chmod +x "$TMP_DIR/gospotify"

log_step "Installing to ${BOLD}${TARGET_BIN}${RESET}..."
if [ "$USE_SUDO" = "1" ]; then
  sudo mv "$TMP_DIR/gospotify" "$TARGET_BIN"
else
  mv "$TMP_DIR/gospotify" "$TARGET_BIN"
fi
log_info "gospotify v${VERSION} installed!"

case ":$PATH:" in
  *":$TARGET_DIR:"*) ;;
  *)
    log_warn "${TARGET_DIR} is not on your PATH. Add it with:"
    printf "     export PATH=\"%s:\$PATH\"\n" "$TARGET_DIR"
    ;;
esac

printf "\nGet started:\n\n"
printf "  1. Create an app at ${BOLD}https://developer.spotify.com/dashboard${RESET}\n"
printf "     (tick Web API + Web Playback SDK; Spotify Premium required)\n"
printf "  2. Add the redirect URI ${AMBER}http://127.0.0.1:7172/callback${RESET}\n"
printf "  3. Run it:\n\n"
printf "  ${AMBER}gospotify -client-id <your client id>${RESET}   # first run; the ID is remembered\n"
printf "  ${AMBER}gospotify${RESET}                               # afterwards\n"
printf "  ${AMBER}gospotify https://open.spotify.com/playlist/…${RESET}  # open any Spotify link\n\n"
