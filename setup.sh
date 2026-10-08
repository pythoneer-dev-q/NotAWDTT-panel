#!/usr/bin/env bash
# ==============================================================================
# WDTT Setup Script — Автоматическая установка MongoDB, ядра WDTT и панели
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

if [ "$(id -u)" -ne 0 ]; then
    echo -e "${RED}[ERROR] Скрипт должен быть запущен под root (sudo bash setup.sh)${NC}"
    exit 1
fi

echo -e "${CYAN}${BOLD}"
echo "═══════════════════════════════════════════════════════"
echo "    Установка и настройка WDTT (RAW/UDP + MongoDB)   "
echo "═══════════════════════════════════════════════════════"
echo -e "${NC}"

ARCH="amd64"
case "$(uname -m)" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo -e "${RED}Архитектура $(uname -m) не поддерживается${NC}"; exit 1 ;;
esac

# 1. Системные пакеты
echo -e "${CYAN}[1/6] Обновление пакетов и установка зависимостей...${NC}"
apt-get update -y
apt-get install -y curl gnupg tar iptables ca-certificates libcap2-bin iproute2 procps

# 2. Установка MongoDB (если не установлена)
echo -e "${CYAN}[2/6] Проверка и установка MongoDB...${NC}"
if ! command -v mongod >/dev/null 2>&1; then
    echo " -> Добавление официального репозитория MongoDB Community..."
    curl -fsSL https://www.mongodb.org/static/pgp/server-7.0.asc | \
        gpg -o /usr/share/keyrings/mongodb-server-7.0.gpg --dearmor --yes 2>/dev/null || true

    . /etc/os-release
    DISTRO="${ID:-ubuntu}"
    CODENAME="${VERSION_CODENAME:-jammy}"

    if [ "$DISTRO" = "ubuntu" ]; then
        echo "deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-7.0.gpg ] https://repo.mongodb.org/apt/ubuntu ${CODENAME}/mongodb-org/7.0 multiverse" | \
            tee /etc/apt/sources.list.d/mongodb-org-7.0.list >/dev/null
    else
        echo "deb [ signed-by=/usr/share/keyrings/mongodb-server-7.0.gpg ] http://repo.mongodb.org/apt/debian ${CODENAME}/mongodb-org/7.0 main" | \
            tee /etc/apt/sources.list.d/mongodb-org-7.0.list >/dev/null
    fi

    apt-get update -y || true
    apt-get install -y mongodb-org || apt-get install -y mongodb || true
fi

# Запуск и включение MongoDB
systemctl daemon-reload
systemctl enable --now mongod 2>/dev/null || systemctl enable --now mongodb 2>/dev/null || true
sleep 2

if systemctl is-active --quiet mongod 2>/dev/null || systemctl is-active --quiet mongodb 2>/dev/null; then
    echo -e "${GREEN}✓ MongoDB успешно запущена и активна!${NC}"
else
    echo -e "${YELLOW}Внимание: не удалось подтвердить статус службы MongoDB. Проверьте: systemctl status mongod${NC}"
fi

# 3. Директории и конфиги
echo -e "${CYAN}[3/6] Подготовка рабочих директорий...${NC}"
mkdir -p /etc/wdtt /etc/wdtt/backups /usr/local/bin /var/log/wdtt
echo "mongodb://127.0.0.1:27017/wdtt" > /etc/wdtt/mongodb.uri

# 4. Сборка / установка бинарника WDTT
echo -e "${CYAN}[4/6] Сборка и установка бинарника WDTT...${NC}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if command -v go >/dev/null 2>&1; then
    echo " -> Сборка unified бинарника через Go..."
    bash ./build.sh "$ARCH" unified || true
fi

BIN_OUT="wdtt-linux-${ARCH}"
if [ -f "$BIN_OUT" ]; then
    install -m 0755 "$BIN_OUT" /usr/local/bin/wdtt-app
elif [ -f "/usr/local/bin/wdtt-app" ]; then
    echo " -> Используется существующий бинарник /usr/local/bin/wdtt-app"
else
    echo -e "${RED}[ERROR] Бинарник $BIN_OUT не найден. Запустите build.sh вручную.${NC}"
    exit 1
fi

# Установка CLI-утилиты
install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-cli.sh" /usr/local/bin/wdtt
cat << 'EOF' > /etc/profile.d/wdtt.sh
alias wdtt='/usr/local/bin/wdtt'
EOF

# 5. Настройка systemd-сервиса
echo -e "${CYAN}[5/6] Создание службы systemd...${NC}"
cat << 'EOF' > /etc/systemd/system/wdtt.service
[Unit]
Description=WDTT Unified VPN and Web Panel (RAW & UDP)
After=network.target mongod.service
Wants=mongod.service

[Service]
Type=simple
User=root
WorkingDirectory=/etc/wdtt
ExecStart=/usr/local/bin/wdtt-app -config-dir /etc/wdtt
Restart=always
RestartSec=3
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable wdtt.service
systemctl restart wdtt.service
sleep 3

# 6. Настройка фаервола
echo -e "${CYAN}[6/6] Настройка фаервола (открытие портов)...${NC}"
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
    ufw allow 2860/tcp comment "WDTT Panel" >/dev/null 2>&1 || true
    ufw allow 2096/tcp comment "WDTT Subscription" >/dev/null 2>&1 || true
    ufw allow 56000/udp comment "WDTT DTLS" >/dev/null 2>&1 || true
    ufw allow 56003/udp comment "WDTT RAW Direct" >/dev/null 2>&1 || true
elif command -v iptables >/dev/null 2>&1; then
    iptables -C INPUT -p tcp --dport 2860 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp --dport 2860 -j ACCEPT
    iptables -C INPUT -p tcp --dport 2096 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp --dport 2096 -j ACCEPT
    iptables -C INPUT -p udp --dport 56000 -j ACCEPT 2>/dev/null || iptables -I INPUT -p udp --dport 56000 -j ACCEPT
    iptables -C INPUT -p udp --dport 56003 -j ACCEPT 2>/dev/null || iptables -I INPUT -p udp --dport 56003 -j ACCEPT
fi

IP=$(curl -4 -s --max-time 3 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')
echo ""
echo -e "${GREEN}${BOLD}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}${BOLD}   ✓ WDTT успешно установлен и запущен!                 ${NC}"
echo -e "${GREEN}${BOLD}═══════════════════════════════════════════════════════${NC}"
echo ""
echo -e " Веб-панель:          ${CYAN}http://${IP}:2860/wdtt/${NC}"
echo -e " Логин по умолчанию:  ${BOLD}admin${NC}"
echo -e " Пароль по умолчанию: ${BOLD}wdtt${NC}"
echo ""
echo -e " Порты подключения:   ${BLUE}56000/udp${NC} (DTLS) | ${BLUE}56003/udp${NC} (RAW Direct)"
echo -e " База данных:         ${GREEN}MongoDB (mongodb://127.0.0.1:27017/wdtt)${NC}"
echo -e " Управление:          ${BOLD}wdtt${NC} (алиас и интерактивное меню)"
echo ""
echo -e "Пример команд:"
echo -e "  ${BOLD}wdtt${NC}          - Меню управления"
echo -e "  ${BOLD}wdtt status${NC}   - Статус сервера и портов"
echo -e "  ${BOLD}wdtt backup${NC}   - Создание бэкапа базы и настроек"
echo -e "  ${BOLD}wdtt logs${NC}     - Просмотр логов"
echo ""

