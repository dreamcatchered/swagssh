#!/usr/bin/env sh
set -e

DOMAIN="ssh.swag.best"
BASE_URL="https://${DOMAIN}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "[-] Unsupported architecture: $ARCH"
        echo "    Supported: x86_64 (amd64), aarch64 (arm64)"
        exit 1
        ;;
esac

BINARY="swagssh-${OS}-${ARCH}"
URL="${BASE_URL}/releases/${BINARY}"
DEST="/tmp/swagssh"

echo ""
echo "  [+] swagSSH Installer"
echo "  [+] Platform: ${OS}/${ARCH}"
echo "  [+] Downloading: ${URL}"
echo ""

if command -v curl > /dev/null 2>&1; then
    curl -fsSL "$URL" -o "$DEST"
elif command -v wget > /dev/null 2>&1; then
    wget -q "$URL" -O "$DEST"
else
    echo "[-] Neither curl nor wget found. Please install one of them."
    exit 1
fi

chmod +x "$DEST"

echo "  [+] Initializing Reverse SSH Tunnel..."
echo ""

exec "$DEST" share --server "${DOMAIN}:2222"
