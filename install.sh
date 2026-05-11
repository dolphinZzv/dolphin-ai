#!/usr/bin/env bash
set -euo pipefail

# ─── Chick Install Script ──────────────────────────────────────────
# Detects platform, checks Go version, builds the binary, and prints
# MCP integration config for Claude Code / OpenCode / Cline.
# ───────────────────────────────────────────────────────────────────

BOLD="\033[1m"
DIM="\033[2m"
GREEN="\033[32m"
YELLOW="\033[33m"
CYAN="\033[36m"
RED="\033[31m"
RESET="\033[0m"

info()  { printf "${BOLD}${GREEN}==>${RESET}${BOLD} %s${RESET}\n" "$*"; }
warn()  { printf "${BOLD}${YELLOW}==>${RESET} %s${RESET}\n" "$*"; }
error() { printf "${BOLD}${RED}==>${RESET}${BOLD} %s${RESET}\n" "$*"; exit 1; }

# ─── Paths ────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${SCRIPT_DIR}/bin"
BINARY="${BUILD_DIR}/chick"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/chick"

# ─── Detect OS ────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"
case "${OS}" in
  Linux)  PLATFORM="linux" ;;
  Darwin) PLATFORM="darwin" ;;
  *)      error "Unsupported OS: ${OS}. Only Linux and macOS are supported." ;;
esac
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) warn "Unknown architecture: ${ARCH}, assuming amd64"; ARCH="amd64" ;;
esac

info "Detected: ${PLATFORM}/${ARCH}"

# ─── Check Go ──────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  error "Go is not installed. Install Go 1.22.5+ from https://go.dev/dl/"
fi

GO_VERSION="$(go version | sed -E 's/.*go([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/')"
GO_MAJOR="$(echo "$GO_VERSION" | cut -d. -f1)"
GO_MINOR="$(echo "$GO_VERSION" | cut -d. -f2)"

if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 22 ]; }; then
  error "Go 1.22.5+ required, found ${GO_VERSION}. Upgrade from https://go.dev/dl/"
fi
if [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -eq 22 ]; then
  GO_PATCH="$(go version | sed -E 's/.*go[0-9]+\.[0-9]+\.([0-9]+).*/\1/')"
  if [ -n "$GO_PATCH" ] && [ "$GO_PATCH" -lt 5 ]; then
    error "Go 1.22.5+ required, found 1.22.${GO_PATCH}. Upgrade from https://go.dev/dl/"
  fi
fi

info "Go ${GO_VERSION} — OK"

# ─── Build ─────────────────────────────────────────────────────────
info "Building..."
mkdir -p "${BUILD_DIR}"
cd "${SCRIPT_DIR}"
go build -race -ldflags="-s -w" -o "${BINARY}" ./cmd/server/
info "Binary: ${BINARY}"

# ─── Config ────────────────────────────────────────────────────────
if [ ! -f "${CONFIG_DIR}/env" ]; then
  mkdir -p "${CONFIG_DIR}"
  cat > "${CONFIG_DIR}/env" <<-ENVEOF
# Chick configuration — sourced by the launch wrapper.
# Override any value via environment variable (CHICK_PORT, CHICK_DB_DRIVER, etc.).

CHICK_DB_DRIVER=sqlite3
CHICK_DB_DSN=file:${CONFIG_DIR}/chick.db
CHICK_PORT=8080
ENVEOF
  info "Config: ${CONFIG_DIR}/env"
else
  info "Config: ${CONFIG_DIR}/env (already exists, skipped)"
fi

# ─── Print Summary ─────────────────────────────────────────────────
printf "\n"
printf "${BOLD}${CYAN}┌──────────────────────────────────────────────────────┐${RESET}\n"
printf "${BOLD}${CYAN}│  Chick Agent Platform                                │${RESET}\n"
printf "${BOLD}${CYAN}│  Binary:  ${BINARY}${RESET}\n"
printf "${BOLD}${CYAN}│  Config:  ${CONFIG_DIR}/env${RESET}\n"
printf "${BOLD}${CYAN}│  Run:     ${BINARY}${RESET}\n"
printf "${BOLD}${CYAN}│  STDIO:   ${BINARY} --stdio${RESET}\n"
printf "${BOLD}${CYAN}└──────────────────────────────────────────────────────┘${RESET}\n"

printf "\n"
printf "${BOLD}Launch (SSE mode):${RESET}\n"
printf "  ${BINARY}\n"
printf "\n"
printf "${BOLD}Launch (STDIO mode for AI assistants):${RESET}\n"
printf "  ${BINARY} --stdio\n"
printf "\n"
printf "${BOLD}Claude Code config (~/.claude/settings.json):${RESET}\n"
printf "  ${DIM}{\"mcpServers\": {\"chick\": {\"command\": \"${BINARY}\", \"args\": [\"--stdio\"]}}}${RESET}\n"
printf "\n"
printf "${BOLD}OpenCode config (~/.config/opencode/opencode.json):${RESET}\n"
printf "  ${DIM}{\"mcpServers\": {\"chick\": {\"command\": \"${BINARY}\", \"args\": [\"--stdio\"]}}}${RESET}\n"
printf "\n"
printf "${BOLD}Cline config (cline_desktop_config.json):${RESET}\n"
printf "  ${DIM}{\"mcpServers\": {\"chick\": {\"command\": \"${BINARY}\", \"args\": [\"--stdio\"]}}}${RESET}\n"
printf "\n"

info "Done. Run '${BINARY}' to start."
