#!/usr/bin/env bash
# ==============================================================================
# ☀️ sunRayPM CLI Quick Installer
#
# Copyright (c) 2026 sunRayPM Contributors & Kai Ali Kutsalcan.
# All rights reserved.
#
# Usage:
#   curl -fsSL https://sunraypm.com/install.sh | bash
#   or locally: bash install.sh
# ==============================================================================

set -e

COLOR_GOLD="\033[38;5;220m"
COLOR_CYAN="\033[36m"
COLOR_GREEN="\033[32m"
COLOR_RED="\033[31m"
COLOR_RESET="\033[0m"

echo -e "${COLOR_GOLD}☀️  sunRay CLI Installer${COLOR_RESET}"
echo -e "Installing developer tools & terminal ASCII visualizers..."

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        echo -e "${COLOR_RED}Unsupported architecture: $ARCH${COLOR_RESET}"
        exit 1
        ;;
esac

TARGET_DIR="/usr/local/bin"
if [ ! -w "$TARGET_DIR" ]; then
    TARGET_DIR="$HOME/.local/bin"
    mkdir -p "$TARGET_DIR"
fi

# 1. If run directly inside the repo folder with main.go present
if [ -f "./main.go" ] && command -v go >/dev/null 2>&1; then
    echo -e "📦 Compiling from current working directory..."
    go build -ldflags="-s -w" -o "$TARGET_DIR/sunray" .
    cp "$TARGET_DIR/sunray" "$TARGET_DIR/sunraypm" 2>/dev/null || true
    echo -e "${COLOR_GREEN}✓ Installed sunray and sunraypm to $TARGET_DIR${COLOR_RESET}"

# 2. If Go is installed on host machine, clone and build
elif command -v go >/dev/null 2>&1; then
    echo -e "📦 Building binary from source repository..."
    TMP_DIR="$(mktemp -d)"
    git clone --depth 1 https://github.com/DaddyChristmas/sunraypm-cli.git "$TMP_DIR/sunraypm-cli" 2>/dev/null || true
    
    if [ -d "$TMP_DIR/sunraypm-cli" ]; then
        cd "$TMP_DIR/sunraypm-cli"
        go build -ldflags="-s -w" -o "$TARGET_DIR/sunray" .
        cp "$TARGET_DIR/sunray" "$TARGET_DIR/sunraypm" 2>/dev/null || true
        rm -rf "$TMP_DIR"
        echo -e "${COLOR_GREEN}✓ Installed sunray and sunraypm to $TARGET_DIR${COLOR_RESET}"
    else
        echo -e "${COLOR_RED}Failed to clone repository. Downloading binary instead...${COLOR_RESET}"
        BINARY_URL="https://github.com/DaddyChristmas/sunraypm-cli/releases/latest/download/sunray-${OS}-${ARCH}"
        curl -fsSL "$BINARY_URL" -o "$TARGET_DIR/sunray" || exit 1
        chmod +x "$TARGET_DIR/sunray"
        cp "$TARGET_DIR/sunray" "$TARGET_DIR/sunraypm" 2>/dev/null || true
        echo -e "${COLOR_GREEN}✓ Installed sunray to $TARGET_DIR${COLOR_RESET}"
    fi

# 3. Fallback: Download prebuilt release binary
else
    echo -e "${COLOR_CYAN}Downloading prebuilt release...${COLOR_RESET}"
    BINARY_URL="https://github.com/DaddyChristmas/sunraypm-cli/releases/latest/download/sunray-${OS}-${ARCH}"
    curl -fsSL "$BINARY_URL" -o "$TARGET_DIR/sunray" || {
        echo -e "${COLOR_RED}Prebuilt binary download failed.${COLOR_RESET}"
        exit 1
    }
    chmod +x "$TARGET_DIR/sunray"
    cp "$TARGET_DIR/sunray" "$TARGET_DIR/sunraypm" 2>/dev/null || true
    echo -e "${COLOR_GREEN}✓ Installed sunray to $TARGET_DIR${COLOR_RESET}"
fi

echo -e "\n${COLOR_GOLD}🎉 Installation Complete!${COLOR_RESET}"
echo -e "Try it out:"
echo -e "  ${COLOR_CYAN}sunray repl${COLOR_RESET}                     # Interactive shell with @ autocompletion"
echo -e "  ${COLOR_CYAN}sunray /draw-kanban @task${COLOR_RESET}        # ASCII Kanban board"
echo -e "  ${COLOR_CYAN}sunray /draw-gantt @space${COLOR_RESET}        # ASCII Gantt timeline"
echo -e "  ${COLOR_CYAN}sunray /draw-tree @parent${COLOR_RESET}        # ASCII Hierarchy DAG tree"
echo -e "  ${COLOR_CYAN}sunray /sunny${COLOR_RESET}                    # Sunny pet companion"
