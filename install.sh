#!/usr/bin/env sh
# zashhomo one-line installer for Linux and macOS.
#   curl -fsSL https://raw.githubusercontent.com/zashhomo/zashhomo/main/install.sh | bash
#
# Environment overrides:
#   ZASHHOMO_REPO   owner/repo (default zashhomo/zashhomo)
#   ZASHHOMO_BIN    install dir (default /usr/local/bin)
#   ZASHHOMO_NO_INSTALL=1  download only; skip `zashhomo install`
set -eu

REPO="${ZASHHOMO_REPO:-zashhomo/zashhomo}"
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

asset="zashhomo-${os}-${arch}"
url="https://github.com/${REPO}/releases/latest/download/${asset}"

# Pick a downloader.
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
else
  err "need curl or wget"
fi

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
