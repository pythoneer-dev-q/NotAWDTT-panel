#!/bin/bash
# Заворот VPN-трафика в Xray: TCP NAT REDIRECT + inner UDP TPROXY на wdtt0/wdtt-raw.
# MTU (MSS + DF-clear): /usr/local/bin/wdtt-mtu-rules.sh — не дублировать здесь.
# Правила привязаны к имени iface и применяются даже если TUN ещё не поднят
# (гонка wdtt-xray ExecStartPost vs startRawTUN). Без UDP DROP: игры и CSQTT должны жить.
# Внешний CSQTT приходит с WAN, не -i wdtt-raw — TPROXY его не трогает.
set +e

WG_IFACE="${WDTT_IFACE:-wdtt0}"
RAW_IFACE="${WDTT_RAW_IFACE:-wdtt-raw}"
XPORT="${WDTT_XRAY_PORT:-12345}"
TPROXY_PORT="${WDTT_TPROXY_PORT:-12346}"
TPROXY_MARK="${WDTT_TPROXY_MARK:-0x66}"
TPROXY_MASK="${WDTT_TPROXY_MASK:-0xff}"
TPROXY_TABLE="${WDTT_TPROXY_TABLE:-166}"
TPROXY_PRIO="${WDTT_TPROXY_PRIO:-10066}"
DNS_IP="${WDTT_DNS_IP:-10.66.66.1}"
WG_NET="${WDTT_TUN_NET:-10.66.66.0/24}"
RAW_NET="${WDTT_RAW_NET:-10.70.0.0/16}"
PANEL_DB="${WDTT_PANEL_DB:-/etc/wdtt/panel.db}"
DEFAULT_RAW_NET="10.70.0.0/16"

# Serialize concurrent up/down (wdtt ExecStartPost + xray ExecStartPost + startRawTUN).
# Lock only under /run (or WDTT_XRAY_RULES_LOCK). Do not fall back to /tmp (symlink race).
if command -v flock >/dev/null 2>&1; then
    _lock="${WDTT_XRAY_RULES_LOCK:-/run/wdtt-xray-rules.lock}"
    if exec 9>"$_lock" 2>/dev/null; then
        flock 9 || true
    fi
fi

# RFC1918 IPv4 CIDR: octets 0-255, prefix /8-/32 (same as installer / wdtt-mtu-rules.sh).
is_private_ipv4_cidr() {
    local cidr="${1:-}"
    local o1 o2 o3 o4 prefix
    [[ "$cidr" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})/([0-9]{1,2})$ ]] || return 1
    o1="${BASH_REMATCH[1]}"; o2="${BASH_REMATCH[2]}"; o3="${BASH_REMATCH[3]}"; o4="${BASH_REMATCH[4]}"
    prefix="${BASH_REMATCH[5]}"
    ((10#$o1 <= 255 && 10#$o2 <= 255 && 10#$o3 <= 255 && 10#$o4 <= 255)) || return 1
    ((10#$prefix >= 8 && 10#$prefix <= 32)) || return 1
    if ((10#$o1 == 10)); then return 0; fi
    if ((10#$o1 == 172 && 10#$o2 >= 16 && 10#$o2 <= 31)); then return 0; fi
    if ((10#$o1 == 192 && 10#$o2 == 168)); then return 0; fi
    return 1
}

# RAW/CSQTT subnet: RFC1918, prefix /16-/29 (panel Go policy), no leading-zero octets.
is_valid_raw_subnet() {
    local cidr="${1:-}"
    local o1 o2 o3 o4 prefix oct
    [[ "$cidr" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})/([0-9]{1,2})$ ]] || return 1
    o1="${BASH_REMATCH[1]}"; o2="${BASH_REMATCH[2]}"; o3="${BASH_REMATCH[3]}"; o4="${BASH_REMATCH[4]}"
    prefix="${BASH_REMATCH[5]}"
    for oct in "$o1" "$o2" "$o3" "$o4" "$prefix"; do
        [[ "$oct" =~ ^0[0-9] ]] && return 1
    done
    ((10#$o1 <= 255 && 10#$o2 <= 255 && 10#$o3 <= 255 && 10#$o4 <= 255)) || return 1
    ((10#$prefix >= 16 && 10#$prefix <= 29)) || return 1
    if ((10#$o1 == 10)); then return 0; fi
    if ((10#$o1 == 172 && 10#$o2 >= 16 && 10#$o2 <= 31)); then return 0; fi
    if ((10#$o1 == 192 && 10#$o2 == 168)); then return 0; fi
    return 1
}

valid_raw_net() {
    is_valid_raw_subnet "$1"
}

load_panel_ports() {
    local db="$PANEL_DB"
    PANEL_PORT="${PANEL_PORT:-}"
    SUB_PORT="${SUB_PORT:-}"
    if [[ -f "$db" ]]; then
        local row=""
        if command -v sqlite3 >/dev/null; then
            row="$(sqlite3 "$db" "SELECT port, sub_port FROM panel_config WHERE id=1;" 2>/dev/null || true)"
        elif command -v python3 >/dev/null; then
            row="$(python3 - "$db" <<'PY' 2>/dev/null || true
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
r = c.execute("SELECT port, sub_port FROM panel_config WHERE id=1").fetchone()
if r:
    print(f"{r[0]}|{r[1]}")
PY
            )"
        fi
        if [[ -n "$row" ]]; then
            IFS='|' read -r db_panel db_sub <<< "$row"
            [[ -n "$db_panel" && "$db_panel" -gt 0 ]] && PANEL_PORT="$db_panel"
            [[ -n "$db_sub" && "$db_sub" -gt 0 ]] && SUB_PORT="$db_sub"
        fi
    fi
    PANEL_PORT="${PANEL_PORT:-${WDTT_PANEL_PORT:-2860}}"
    SUB_PORT="${SUB_PORT:-${WDTT_SUB_PORT:-2096}}"
}

load_raw_net() {
    local candidate="${WDTT_RAW_NET:-}"
    if [[ -z "$candidate" ]]; then
        local db="$PANEL_DB"
        local row=""
        if [[ -f "$db" ]]; then
            if command -v sqlite3 >/dev/null; then
                row="$(sqlite3 "$db" "SELECT raw_subnet FROM wdtt_inbound WHERE id=1;" 2>/dev/null || true)"
            elif command -v python3 >/dev/null; then
                row="$(python3 - "$db" <<'PY' 2>/dev/null || true
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
r = c.execute("SELECT raw_subnet FROM wdtt_inbound WHERE id=1").fetchone()
if r and r[0]:
    print(r[0])
PY
                )"
            fi
        fi
        candidate="$(printf '%s' "$row" | tr -d '[:space:]')"
    fi
    if valid_raw_net "$candidate"; then
        RAW_NET="$candidate"
    else
        RAW_NET="${DEFAULT_RAW_NET}"
    fi
    export WDTT_RAW_NET="$RAW_NET"
}

load_panel_ports
load_raw_net

cleanup_iface() {
    local iface="$1"
    while iptables -t nat -C PREROUTING -i "$iface" -j XRAY_REDIRECT 2>/dev/null; do
        iptables -t nat -D PREROUTING -i "$iface" -j XRAY_REDIRECT 2>/dev/null || break
    done
    while iptables -t nat -C PREROUTING -i "$iface" -p udp --dport 53 -j DNAT --to-destination "${DNS_IP}:53" 2>/dev/null; do
        iptables -t nat -D PREROUTING -i "$iface" -p udp --dport 53 -j DNAT --to-destination "${DNS_IP}:53" 2>/dev/null || break
    done
    while iptables -C INPUT -i "$iface" -m comment --comment WDTT_XRAY -j ACCEPT 2>/dev/null; do
        iptables -D INPUT -i "$iface" -m comment --comment WDTT_XRAY -j ACCEPT 2>/dev/null || break
    done
    while iptables -t mangle -C PREROUTING -i "$iface" -p udp -j XRAY_TPROXY 2>/dev/null; do
        iptables -t mangle -D PREROUTING -i "$iface" -p udp -j XRAY_TPROXY 2>/dev/null || break
    done
}

cleanup_tproxy_route() {
    local i
    for i in 1 2 3 4 5 6 7 8; do
        ip -4 rule del fwmark "${TPROXY_MARK}/${TPROXY_MASK}" table "$TPROXY_TABLE" priority "$TPROXY_PRIO" 2>/dev/null || break
    done
    ip -4 route del local 0.0.0.0/0 dev lo table "$TPROXY_TABLE" 2>/dev/null || true
}

cleanup() {
    cleanup_iface "$WG_IFACE"
    cleanup_iface "$RAW_IFACE"
    iptables -t nat -F XRAY_REDIRECT 2>/dev/null
    iptables -t nat -X XRAY_REDIRECT 2>/dev/null
    iptables -t mangle -F XRAY_TPROXY 2>/dev/null
    iptables -t mangle -X XRAY_TPROXY 2>/dev/null
    cleanup_tproxy_route
}

probe_tproxy_target() {
    modprobe xt_TPROXY 2>/dev/null || true
    modprobe xt_mark 2>/dev/null || true
    local chain="WDTT_TPROXY_CHECK"
    iptables -t mangle -F "$chain" 2>/dev/null || true
    iptables -t mangle -X "$chain" 2>/dev/null || true
    iptables -t mangle -N "$chain" 2>/dev/null || return 1
    if ! iptables -t mangle -A "$chain" -p udp -j TPROXY \
        --on-port "$TPROXY_PORT" --on-ip 127.0.0.1 \
        --tproxy-mark "${TPROXY_MARK}/${TPROXY_MASK}" 2>/dev/null; then
        iptables -t mangle -F "$chain" 2>/dev/null || true
        iptables -t mangle -X "$chain" 2>/dev/null || true
        return 1
    fi
    iptables -t mangle -F "$chain" 2>/dev/null || true
    iptables -t mangle -X "$chain" 2>/dev/null || true
    return 0
}

if [ "${1:-}" = "down" ]; then
    cleanup
    echo "wdtt-xray rules removed"
    exit 0
fi

HAS_TPROXY=1
if ! probe_tproxy_target; then
    echo "wdtt-xray rules WARNING: kernel/iptables TPROXY target is unavailable; inner UDP TPROXY skipped" >&2
    HAS_TPROXY=0
fi

cleanup

# Цепочка REDIRECT (общая для обоих TUN). TCP REDIRECT; UDP 53 — DNAT.
iptables -t nat -N XRAY_REDIRECT
iptables -t nat -A XRAY_REDIRECT -d "$WG_NET" -j RETURN
iptables -t nat -A XRAY_REDIRECT -d "$RAW_NET" -j RETURN
iptables -t nat -A XRAY_REDIRECT -d 127.0.0.0/8 -j RETURN
iptables -t nat -A XRAY_REDIRECT -d 255.255.255.255/32 -j RETURN
iptables -t nat -A XRAY_REDIRECT -p tcp --dport "$PANEL_PORT" -j RETURN
iptables -t nat -A XRAY_REDIRECT -p tcp --dport "$SUB_PORT" -j RETURN
iptables -t nat -A XRAY_REDIRECT -m addrtype --dst-type LOCAL -j RETURN
iptables -t nat -A XRAY_REDIRECT -p udp --dport 53 -j RETURN
iptables -t nat -A XRAY_REDIRECT -p tcp -j REDIRECT --to-ports "$XPORT"

if [[ "$HAS_TPROXY" -eq 1 ]]; then
    # Inner UDP TPROXY: только -i wdtt0 / wdtt-raw. WAN/CSQTT снаружи не попадает.
    iptables -t mangle -N XRAY_TPROXY
    iptables -t mangle -A XRAY_TPROXY -p udp --dport 53 -j RETURN
    iptables -t mangle -A XRAY_TPROXY -d "$WG_NET" -j RETURN
    iptables -t mangle -A XRAY_TPROXY -d "$RAW_NET" -j RETURN
    iptables -t mangle -A XRAY_TPROXY -d 127.0.0.0/8 -j RETURN
    iptables -t mangle -A XRAY_TPROXY -m addrtype --dst-type LOCAL -j RETURN
    iptables -t mangle -A XRAY_TPROXY -d 224.0.0.0/4 -j RETURN
    iptables -t mangle -A XRAY_TPROXY -d 255.255.255.255/32 -j RETURN
    if ! iptables -t mangle -A XRAY_TPROXY -p udp -j TPROXY \
        --on-port "$TPROXY_PORT" --on-ip 127.0.0.1 \
        --tproxy-mark "${TPROXY_MARK}/${TPROXY_MASK}"; then
        echo "wdtt-xray rules ERROR: failed to install XRAY_TPROXY target" >&2
        cleanup
        exit 1
    fi

    if ! ip -4 rule add fwmark "${TPROXY_MARK}/${TPROXY_MASK}" table "$TPROXY_TABLE" priority "$TPROXY_PRIO" 2>/dev/null; then
        echo "wdtt-xray rules ERROR: failed to install fwmark policy rule" >&2
        cleanup
        exit 1
    fi
    if ! ip -4 route replace local 0.0.0.0/0 dev lo table "$TPROXY_TABLE" 2>/dev/null; then
        echo "wdtt-xray rules ERROR: failed to install local TPROXY route" >&2
        cleanup
        exit 1
    fi
fi

APPLIED=""
apply_iface() {
    local iface="$1"
    # Имя iface, не ifindex: правила живут после ip link del/add wdtt-raw.
    # Не пропускаем, если link ещё down — иначе CSQTT уходит в MASQUERADE мимо Xray.
    iptables -C INPUT -i "$iface" -m comment --comment WDTT_XRAY -j ACCEPT 2>/dev/null || \
        iptables -I INPUT 1 -i "$iface" -m comment --comment WDTT_XRAY -j ACCEPT
    iptables -t nat -C PREROUTING -i "$iface" -p udp --dport 53 -j DNAT --to-destination "${DNS_IP}:53" 2>/dev/null || \
        iptables -t nat -I PREROUTING 1 -i "$iface" -p udp --dport 53 -j DNAT --to-destination "${DNS_IP}:53"
    iptables -t nat -C PREROUTING -i "$iface" -j XRAY_REDIRECT 2>/dev/null || \
        iptables -t nat -A PREROUTING -i "$iface" -j XRAY_REDIRECT
    if [[ "$HAS_TPROXY" -eq 1 ]]; then
        if ! iptables -t mangle -C PREROUTING -i "$iface" -p udp -j XRAY_TPROXY 2>/dev/null &&
           ! iptables -t mangle -A PREROUTING -i "$iface" -p udp -j XRAY_TPROXY; then
            echo "wdtt-xray rules ERROR: failed to attach XRAY_TPROXY to ${iface}" >&2
            return 1
        fi
    fi
    if [[ -n "$APPLIED" ]]; then
        APPLIED="${APPLIED}+${iface}"
    else
        APPLIED="$iface"
    fi
    return 0
}

APPLY_FAILED=0
apply_iface "$WG_IFACE" || APPLY_FAILED=1
apply_iface "$RAW_IFACE" || APPLY_FAILED=1
if [[ "$APPLY_FAILED" -ne 0 ]]; then
    cleanup
    exit 1
fi

if [[ -x /usr/local/bin/wdtt-mtu-rules.sh ]]; then
    WDTT_RAW_NET="$RAW_NET" /usr/local/bin/wdtt-mtu-rules.sh up
fi

if [[ -z "$APPLIED" ]]; then
    echo "wdtt-xray rules: no VPN ifaces applied"
    exit 0
fi
echo "wdtt-xray rules applied (ifaces:${APPLIED} raw_net:$RAW_NET panel:$PANEL_PORT sub:$SUB_PORT TCP -> :$XPORT, UDP TPROXY -> :$TPROXY_PORT, DNS -> $DNS_IP:53)"
