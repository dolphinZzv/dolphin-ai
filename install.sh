#!/usr/bin/env bash
set -euo pipefail

REPO="dolphinZzv/dolphin-ai"
VERSION="${1:-latest}"

if [ "$VERSION" = "latest" ]; then
	VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
fi

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
	x86_64|amd64) ARCH="amd64" ;;
	aarch64|arm64) ARCH="arm64" ;;
esac

echo "Downloading Dolphin $VERSION ($OS/$ARCH)..."

curl -fsSL "https://github.com/$REPO/releases/download/$VERSION/dolphin_${VERSION#v}_${OS}_${ARCH}.tar.gz" \
	-o /tmp/dolphin.tar.gz

tar xzf /tmp/dolphin.tar.gz -C /tmp

install_path="/usr/local/bin/dolphin"
if [ -w /usr/local/bin ]; then
	cp /tmp/dolphin "$install_path"
else
	sudo cp /tmp/dolphin "$install_path"
fi

rm -f /tmp/dolphin.tar.gz /tmp/dolphin
echo "Installed to $install_path"
