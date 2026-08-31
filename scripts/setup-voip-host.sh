#!/usr/bin/env bash
# setup-voip-host.sh — idempotent host preparation for VoIP / SIP / RTP.
#
# Configures UFW (or firewalld / iptables) to permit SIP+RTP UDP traffic and
# bumps kernel socket receive buffers. Safe to re-run.
#
# See docs/deployment/SIP_RTP_FIREWALL.md for rationale.

set -euo pipefail

# Defaults match infra/config/config.go. Range 10000-59999 = 50000 ports =
# ~25000 concurrent calls (2 ports/call, even+odd). SIP_TRUNK port range
# (15060-15159 by default) is for per-trunk SIP listeners.
RTP_START="${SIP_TRUNK_RTP_PORT_START:-10000}"
RTP_END="${SIP_TRUNK_RTP_PORT_END:-59999}"
SIP_PORT="${SIP_PORT:-5060}"
SIP_TRUNK_START="${SIP_TRUNK_PORT_START:-15060}"
SIP_TRUNK_COUNT="${SIP_TRUNK_PORT_COUNT:-100}"
SIP_TRUNK_END=$((SIP_TRUNK_START + SIP_TRUNK_COUNT - 1))

RMEM_MAX="${RMEM_MAX:-8388608}"        # 8 MiB
RMEM_DEFAULT="${RMEM_DEFAULT:-1048576}" # 1 MiB

if [[ $EUID -ne 0 ]]; then
  echo "error: this script must be run as root (use sudo)." >&2
  exit 1
fi

log() { printf '[setup-voip-host] %s\n' "$*"; }

# ---------------------------------------------------------------------------
# 1. Firewall rules
# ---------------------------------------------------------------------------

apply_ufw() {
  log "UFW detected — applying allow rules"
  ufw allow proto udp from any to any port "${SIP_PORT}"                              comment 'SIP signaling'        >/dev/null
  ufw allow proto udp from any to any port "${SIP_TRUNK_START}:${SIP_TRUNK_END}"      comment 'SIP trunk listeners'  >/dev/null
  ufw allow proto udp from any to any port "${RTP_START}:${RTP_END}"                  comment 'SIP RTP media'        >/dev/null
  ufw reload >/dev/null
  ufw status verbose | grep -E "${SIP_PORT}|${SIP_TRUNK_START}|${RTP_START}" || true
}

apply_firewalld() {
  log "firewalld detected — applying allow rules"
  firewall-cmd --permanent --add-port="${SIP_PORT}/udp"                                  >/dev/null
  firewall-cmd --permanent --add-port="${SIP_TRUNK_START}-${SIP_TRUNK_END}/udp"          >/dev/null
  firewall-cmd --permanent --add-port="${RTP_START}-${RTP_END}/udp"                      >/dev/null
  firewall-cmd --reload                                                                  >/dev/null
  firewall-cmd --list-ports
}

apply_iptables() {
  log "no UFW/firewalld — falling back to raw iptables"
  iptables -C INPUT -p udp --dport "${SIP_PORT}" -j ACCEPT 2>/dev/null \
    || iptables -A INPUT -p udp --dport "${SIP_PORT}" -j ACCEPT
  iptables -C INPUT -p udp --dport "${SIP_TRUNK_START}:${SIP_TRUNK_END}" -j ACCEPT 2>/dev/null \
    || iptables -A INPUT -p udp --dport "${SIP_TRUNK_START}:${SIP_TRUNK_END}" -j ACCEPT
  iptables -C INPUT -p udp --dport "${RTP_START}:${RTP_END}" -j ACCEPT 2>/dev/null \
    || iptables -A INPUT -p udp --dport "${RTP_START}:${RTP_END}" -j ACCEPT
  if command -v netfilter-persistent >/dev/null 2>&1; then
    netfilter-persistent save >/dev/null
  elif [[ -d /etc/iptables ]]; then
    iptables-save > /etc/iptables/rules.v4
    log "rules persisted to /etc/iptables/rules.v4"
  else
    log "WARNING: could not persist iptables rules (no netfilter-persistent, no /etc/iptables)"
  fi
}

if command -v ufw >/dev/null 2>&1 && ufw status >/dev/null 2>&1; then
  apply_ufw
elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  apply_firewalld
elif command -v iptables >/dev/null 2>&1; then
  apply_iptables
else
  log "WARNING: no recognized firewall manager found — skipping firewall step"
fi

# ---------------------------------------------------------------------------
# 2. Kernel socket-buffer tuning (persisted)
# ---------------------------------------------------------------------------

SYSCTL_FILE=/etc/sysctl.d/99-rtp-rmem.conf
log "writing ${SYSCTL_FILE}"
cat > "${SYSCTL_FILE}" <<EOF
# Managed by scripts/setup-voip-host.sh — RTP burst absorption
net.core.rmem_max=${RMEM_MAX}
net.core.rmem_default=${RMEM_DEFAULT}
EOF

sysctl -w "net.core.rmem_max=${RMEM_MAX}"         >/dev/null
sysctl -w "net.core.rmem_default=${RMEM_DEFAULT}" >/dev/null

log "applied: net.core.rmem_max=$(sysctl -n net.core.rmem_max), net.core.rmem_default=$(sysctl -n net.core.rmem_default)"
log "done."
