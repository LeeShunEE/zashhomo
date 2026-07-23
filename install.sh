#!/usr/bin/env sh
# zashhomo one-line installer for Linux and macOS.
#   curl -fsSL https://raw.githubusercontent.com/LeeShunEE/zashhomo/main/install.sh | bash
#
# Environment overrides:
#   ZASHHOMO_REPO   owner/repo (default LeeShunEE/zashhomo)
#   ZASHHOMO_BIN    install dir (default /usr/local/bin)
#   ZASHHOMO_VERSION  release tag to install (default: latest)
#   ZASHHOMO_NO_INSTALL=1  download only; skip `zashhomo install`
set -eu

REPO="${ZASHHOMO_REPO:-LeeShunEE/zashhomo}"
BIN_DIR="${ZASHHOMO_BIN:-/usr/local/bin}"

info() { printf '\033[1;34m•\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# Detect OS.
os="$(uname -s)"
case "$os" in
  Linux)  os="linux" ;;
  Darwin) os="darwin" ;;
  *) err "unsupported OS: $os" ;;
esac

# Detect architecture.
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)  arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  armv7l|armv7)  arch="arm" ;;
  *) err "unsupported architecture: $arch" ;;
esac

# Pick a downloader. dl writes to a file; fetch writes to stdout.
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
  fetch() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
  fetch() { wget -qO- "$1"; }
else
  err "need curl or wget"
fi

# Resolve the release tag. Assets are named with the version embedded
# (zashhomo-<version>-<os>-<arch>), so we can't use the fixed
# /releases/latest/download/ path and must look up the tag first.
version="${ZASHHOMO_VERSION:-}"
if [ -z "$version" ]; then
  info "Resolving latest release…"
  version="$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' \
    | sed -E 's/.*"tag_name" *: *"([^"]+)".*/\1/')"
  [ -n "$version" ] || err "could not resolve latest release tag"
fi

asset="zashhomo-${version}-${os}-${arch}"
url="https://github.com/${REPO}/releases/download/${version}/${asset}"

# Elevate for writes to system dirs when needed.
SUDO=""
if [ ! -w "$BIN_DIR" ] 2>/dev/null; then
  if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  fi
fi

tmp="$(mktemp)"
info "Downloading ${asset} from ${REPO}…"
dl "$url" "$tmp" || err "download failed: $url"

info "Installing to ${BIN_DIR}/zashhomo"
$SUDO mkdir -p "$BIN_DIR"
$SUDO install -m 0755 "$tmp" "${BIN_DIR}/zashhomo"
rm -f "$tmp"

if [ "${ZASHHOMO_NO_INSTALL:-0}" = "1" ]; then
  info "Downloaded. Run: zashhomo install"
  exit 0
fi

info "Running zashhomo install…"
$SUDO "${BIN_DIR}/zashhomo" install

printf '\n\033[1;32m✓ Done.\033[0m Manage with: zashhomo status | zashhomo sub add <url>\n'
