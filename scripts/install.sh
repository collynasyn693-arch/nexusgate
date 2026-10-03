#!/bin/sh
set -e

# NexusGate Unprivileged Termux Installation & Service Initialization Script
# Installs nexusgate binary, default configurations, and runit service hooks.

echo "=========================================================="
echo " NexusGate Edge Reverse Proxy Installer for Termux ARM64"
echo "=========================================================="

# 1. Determine installation prefixes
if [ -n "${PREFIX}" ] && [ -d "${PREFIX}" ]; then
    INSTALL_BIN="${PREFIX}/bin"
    INSTALL_ETC="${PREFIX}/etc/nexusgate"
    INSTALL_VAR="${PREFIX}/var/run/nexusgate"
    SERVICE_DIR="${PREFIX}/var/service"
else
    INSTALL_BIN="${HOME}/.local/bin"
    INSTALL_ETC="${HOME}/.nexusgate"
    INSTALL_VAR="${HOME}/.nexusgate/run"
    SERVICE_DIR=""
fi

echo "[*] Target Binary Directory:  ${INSTALL_BIN}"
echo "[*] Target Config Directory:  ${INSTALL_ETC}"
echo "[*] Target Runtime Directory: ${INSTALL_VAR}"

mkdir -p "${INSTALL_BIN}" "${INSTALL_ETC}" "${INSTALL_VAR}"
chmod 0700 "${INSTALL_ETC}" "${INSTALL_VAR}"

# 2. Locate or compile nexusgate binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILT_BIN="${ROOT_DIR}/bin/nexusgate"

if [ ! -f "${BUILT_BIN}" ]; then
    echo "[*] Pre-built binary not found; triggering native build..."
    "${SCRIPT_DIR}/build_termux.sh"
fi

if [ -f "${BUILT_BIN}" ]; then
    cp -f "${BUILT_BIN}" "${INSTALL_BIN}/nexusgate"
    chmod 0755 "${INSTALL_BIN}/nexusgate"
    echo "[✓] Installed nexusgate binary to ${INSTALL_BIN}/nexusgate"
else
    echo "[✗] Error: Unable to locate or build nexusgate binary" >&2
    exit 1
fi

# 3. Initialize default configuration if not already present
DEFAULT_CONF="${INSTALL_ETC}/nexusgate.yaml"
if [ ! -f "${DEFAULT_CONF}" ]; then
    if [ -f "${ROOT_DIR}/nexusgate.example.yaml" ]; then
        cp "${ROOT_DIR}/nexusgate.example.yaml" "${DEFAULT_CONF}"
        chmod 0600 "${DEFAULT_CONF}"
        echo "[✓] Initialized default configuration at ${DEFAULT_CONF}"
    fi
else
    echo "[✓] Existing configuration retained at ${DEFAULT_CONF}"
fi

# 4. Optional: Set up termux-services (sv) daemon definition if installed
if [ -n "${SERVICE_DIR}" ] && [ -d "${SERVICE_DIR}" ]; then
    SVC_PATH="${SERVICE_DIR}/nexusgate"
    mkdir -p "${SVC_PATH}"
    cat << SVCEOF > "${SVC_PATH}/run"
#!/bin/sh
exec 2>&1
exec nexusgate start -c "${DEFAULT_CONF}" -s "${INSTALL_VAR}/nexusgate.sock"
SVCEOF
    chmod 0755 "${SVC_PATH}/run"
    echo "[✓] Registered termux-services daemon hook at ${SVC_PATH}/run"
fi

# 5. Verify installation
echo "----------------------------------------------------------"
"${INSTALL_BIN}/nexusgate" version || true
echo "----------------------------------------------------------"
echo "[✓] Installation Complete!"
echo "    Start Gateway:    nexusgate start -c ${DEFAULT_CONF}"
echo "    Interactive TUI:  nexusgate run -c ${DEFAULT_CONF}"
echo "    Attach Monitor:   nexusgate attach"
echo "=========================================================="
