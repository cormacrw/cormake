#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

make build
install -m 755 cormake "$INSTALL_DIR/cormake"

echo "installed to $INSTALL_DIR/cormake"
