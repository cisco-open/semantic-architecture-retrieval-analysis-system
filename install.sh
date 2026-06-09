#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 Cisco Systems, Inc. and its affiliates
#
# See CONTRIBUTORS.md for full contributor list.

set -euo pipefail

# SARAS installer script
# Usage: curl -sSfL https://raw.githubusercontent.com/cisco-open/semantic-architecture-retrieval-analysis-system/main/install.sh | bash

REPO="cisco-open/semantic-architecture-retrieval-analysis-system"
BINARY="saras"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    mingw*|msys*|cygwin*) OS="windows" ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Get latest version
echo "Fetching latest release..."
LATEST=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
    echo "Error: Could not determine latest version"
    exit 1
fi

VERSION="${LATEST#v}"
echo "Latest version: ${LATEST}"

# Build download URL
EXT="tar.gz"
if [ "$OS" = "windows" ]; then
    EXT="zip"
fi

FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${FILENAME}"

# Download
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

echo "Downloading ${URL}..."
curl -sSfL -o "${TMPDIR}/${FILENAME}" "$URL"

# Extract
echo "Extracting..."
if [ "$EXT" = "zip" ]; then
    unzip -q "${TMPDIR}/${FILENAME}" -d "$TMPDIR"
else
    tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"
fi

# Install
echo "Installing to ${INSTALL_DIR}/${BINARY}..."
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
    sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

echo ""
echo "Successfully installed ${BINARY} ${LATEST} to ${INSTALL_DIR}/${BINARY}"
echo "Run 'saras --help' to get started."
