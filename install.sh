#!/usr/bin/env bash
# ==============================================================================
# install.sh (install_packages.sh 실행 래퍼)
# ==============================================================================
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
exec bash "$SCRIPT_DIR/install_packages.sh" "$@"
