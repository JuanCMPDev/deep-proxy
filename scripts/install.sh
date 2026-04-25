#!/usr/bin/env sh
# deep-proxy installer — works on Linux and macOS.
# Usage: curl -fsSL https://raw.githubusercontent.com/JuanCMPDev/deep-proxy/main/scripts/install.sh | sh
set -e

REPO="JuanCMPDev/deep-proxy"
BINARY="deep-proxy"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# ── Detect OS ─────────────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS — download the Windows binary from GitHub Releases." && exit 1 ;;
esac

# ── Detect arch ───────────────────────────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

# ── Resolve latest version ────────────────────────────────────────────────────
if [ -n "$VERSION" ]; then
  TAG="$VERSION"
else
  TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
fi

if [ -z "$TAG" ]; then
  echo "error: could not determine latest release (check your internet connection or set VERSION=vX.Y.Z)" && exit 1
fi

ARCHIVE="${BINARY}_${TAG#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

echo "Installing ${BINARY} ${TAG} (${OS}/${ARCH})..."

# ── Download and verify ───────────────────────────────────────────────────────
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "$TMPDIR/$ARCHIVE"
curl -fsSL "$CHECKSUM_URL" -o "$TMPDIR/checksums.txt"

cd "$TMPDIR"
# Verify SHA-256 if sha256sum or shasum is available.
if command -v sha256sum >/dev/null 2>&1; then
  grep "$ARCHIVE" checksums.txt | sha256sum --check --status
elif command -v shasum >/dev/null 2>&1; then
  grep "$ARCHIVE" checksums.txt | shasum -a 256 --check --status
else
  echo "warning: skipping checksum verification (sha256sum/shasum not found)"
fi

tar -xzf "$ARCHIVE"

# ── Install ───────────────────────────────────────────────────────────────────
if [ -w "$INSTALL_DIR" ]; then
  install -m755 "$BINARY" "$INSTALL_DIR/$BINARY"
else
  echo "Installing to $INSTALL_DIR (requires sudo)..."
  sudo install -m755 "$BINARY" "$INSTALL_DIR/$BINARY"
fi

echo ""
echo "✓ ${BINARY} ${TAG} installed to ${INSTALL_DIR}/${BINARY}"
echo ""
echo "Next steps:"
echo "  export DEEPPROXY_TOKEN='your-deepseek-session-token'"
echo "  ${BINARY} start"
