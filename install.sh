#!/usr/bin/env bash
# install.sh — Automated installer for Beelzebub (Self-Healing Homelab Sentinel)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SOURCE="${SCRIPT_DIR}/dist/sentinel"
BIN_TARGET="/usr/local/bin/sentinel"
CONFIG_DIR="/etc/beelzebub"
CONFIG_FILE="${CONFIG_DIR}/sentinels.yaml"
DATA_DIR="/var/lib/beelzebub"
SERVICE_SOURCE="${SCRIPT_DIR}/packaging/sentinel.service"
SERVICE_TARGET="/etc/systemd/system/sentinel.service"

# Colour helpers
if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-}" != "dumb" ]]; then
  GREEN=$'\033[0;32m'; YELLOW=$'\033[1;33m'; RED=$'\033[0;31m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
else
  GREEN=''; YELLOW=''; RED=''; BOLD=''; RESET=''
fi

info()    { printf "${GREEN}[OK]${RESET}    %s\n" "$*"; }
warn()    { printf "${YELLOW}[WARN]${RESET}  %s\n" "$*"; }
error()   { printf "${RED}[ERR]${RESET}   %s\n" "$*" >&2; }
heading() { printf "\n${BOLD}%s${RESET}\n" "$*"; }

check_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    error "Installation requires root privileges. Please run with sudo:"
    echo "  sudo ./install.sh"
    exit 1
  fi
}

do_build() {
  heading "1. Building standalone static binary"
  if [[ ! -f "${BIN_SOURCE}" ]]; then
    "${SCRIPT_DIR}/scripts/build.sh"
  else
    info "Found pre-built binary at ${BIN_SOURCE}. Rebuilding to ensure latest version..."
    "${SCRIPT_DIR}/scripts/build.sh"
  fi
}

do_install() {
  check_root

  heading "2. Installing binary"
  install -Dm755 "${BIN_SOURCE}" "${BIN_TARGET}"
  info "Installed ${BIN_TARGET}"

  heading "3. Configuring paths and defaults"
  mkdir -p "${CONFIG_DIR}" "${DATA_DIR}"
  chmod 755 "${CONFIG_DIR}" "${DATA_DIR}"
  info "Created data directory: ${DATA_DIR}"

  if [[ ! -f "${CONFIG_FILE}" ]]; then
    cp "${SCRIPT_DIR}/sentinels.yaml" "${CONFIG_FILE}"
    chmod 644 "${CONFIG_FILE}"
    info "Created default configuration: ${CONFIG_FILE}"
  else
    info "Existing configuration preserved at ${CONFIG_FILE}"
  fi

  heading "4. Installing systemd service"
  install -Dm644 "${SERVICE_SOURCE}" "${SERVICE_TARGET}"
  info "Installed ${SERVICE_TARGET}"
  systemctl daemon-reload
  info "systemd reloaded"

  heading "5. Service status & activation"
  echo "Installation complete!"
  echo ""
  echo "To enable and start Beelzebub daemon now, run:"
  echo "  sudo systemctl enable --now sentinel.service"
  echo ""
  echo "To launch the interactive centered TUI cockpit at any time, run:"
  echo "  sentinel"
  echo ""
  echo "To check daemon logs:"
  echo "  journalctl -u sentinel.service -f"
}

do_uninstall() {
  check_root
  heading "Uninstalling Beelzebub"

  if systemctl is-active --quiet sentinel.service 2>/dev/null; then
    warn "Stopping sentinel service..."
    systemctl stop sentinel.service
  fi

  if systemctl is-enabled --quiet sentinel.service 2>/dev/null; then
    warn "Disabling sentinel service..."
    systemctl disable sentinel.service
  fi

  if [[ -f "${SERVICE_TARGET}" ]]; then
    rm -f "${SERVICE_TARGET}"
    systemctl daemon-reload
    info "Removed ${SERVICE_TARGET}"
  fi

  if [[ -f "${BIN_TARGET}" ]]; then
    rm -f "${BIN_TARGET}"
    info "Removed ${BIN_TARGET}"
  fi

  warn "Configuration (${CONFIG_DIR}) and data (${DATA_DIR}) were preserved."
  warn "To remove them manually:"
  echo "  sudo rm -rf ${CONFIG_DIR} ${DATA_DIR}"
  info "Uninstallation finished."
}

# CLI Argument handling
case "${1:-install}" in
  install)
    do_build
    do_install
    ;;
  uninstall|--uninstall)
    do_uninstall
    ;;
  build)
    do_build
    ;;
  help|-h|--help)
    echo "Usage: ./install.sh [install|uninstall|build|help]"
    echo ""
    echo "Commands:"
    echo "  install    Build and install binary, systemd service, and config (default)"
    echo "  uninstall  Remove systemd service and binary"
    echo "  build      Only compile the static binary"
    echo "  help       Show this help message"
    ;;
  *)
    error "Unknown option: $1"
    echo "Run './install.sh help' for usage."
    exit 1
    ;;
esac
