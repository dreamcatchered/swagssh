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

if [ -z "$XDG_BIN_HOME" ]; then
    XDG_BIN_HOME="$HOME/.local/bin"
fi

INSTALL_DIR="$XDG_BIN_HOME"
EXE_PATH="$INSTALL_DIR/swagssh"

echo ""
echo "  [+] swagSSH Installer"
echo "  [+] Platform: ${OS}/${ARCH}"
echo "  [+] Installing to: ${INSTALL_DIR}"
echo "  [+] Downloading: ${URL}"
echo ""

mkdir -p "$INSTALL_DIR" 2>/dev/null || true

if command -v curl > /dev/null 2>&1; then
    curl -fsSL "$URL" -o "$EXE_PATH"
elif command -v wget > /dev/null 2>&1; then
    wget -q "$URL" -O "$EXE_PATH"
else
    echo "[-] Neither curl nor wget found. Please install one of them."
    exit 1
fi

chmod +x "$EXE_PATH"

case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        ;;
    *)
        echo "  [+] Adding ${INSTALL_DIR} to PATH"
        echo "  [+] Run this to update your shell:"
        echo ""
        echo "      export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
        if [ -f "$HOME/.bashrc" ]; then
            echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$HOME/.bashrc"
        fi
        if [ -f "$HOME/.zshrc" ]; then
            echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$HOME/.zshrc"
        fi
        if [ -f "$HOME/.profile" ]; then
            echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$HOME/.profile"
        fi
        export PATH="$HOME/.local/bin:$PATH"
        ;;
esac

echo "  [+] Installed to: $EXE_PATH"
echo "  [+] To connect from another terminal: swagssh connect <session-id>"
echo "  [+] (Open a new terminal if PATH was just updated)"
echo ""
echo "  [+] Initializing Reverse SSH Tunnel..."
echo ""

exec "$EXE_PATH" share --server "${DOMAIN}:2222"
