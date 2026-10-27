#!/usr/bin/env bash
# ==============================================================================
# WDTT Setup Script — Автоматическая установка MongoDB, ядра WDTT и панели
# ==============================================================================
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_MODE="${WDTT_INSTALL_MODE:-}"
case "${1:-}" in
    --panel-only) INSTALL_MODE="panel" ;;
    --panel-node) INSTALL_MODE="panel-node" ;;
    "") ;;
    *) echo "Использование: sudo bash setup.sh [--panel-only|--panel-node]" >&2; exit 2 ;;
esac
if [ -z "$INSTALL_MODE" ]; then
    if [ -t 0 ]; then
        echo "Режим установки: 1) Только панель  2) Панель + нода"
        read -rp "Выберите [1/2] (по умолчанию 2): " mode_choice
    elif [ -t 2 ] && [ -r /dev/tty ]; then
        echo "Режим установки: 1) Только панель  2) Панель + нода" >&2
        read -rp "Выберите [1/2] (по умолчанию 2): " mode_choice < /dev/tty
    else
        mode_choice=2
    fi
    case "$mode_choice" in
        1) INSTALL_MODE="panel" ;;
        ""|2) INSTALL_MODE="panel-node" ;;
        *) echo "Неверный режим установки" >&2; exit 2 ;;
    esac
fi
case "$INSTALL_MODE" in
    panel|panel-node) ;;
    *) echo "WDTT_INSTALL_MODE должен быть panel или panel-node" >&2; exit 2 ;;
esac

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
# Очищаем устаревшие или битые репозитории MongoDB перед обновлением
rm -f /etc/apt/sources.list.d/mongodb-org-*.list

apt-get update -y || true
apt-get install -y curl gnupg tar iptables ca-certificates libcap2-bin iproute2 procps

# 2. Установка MongoDB (если не установлена)
echo -e "${CYAN}[2/6] Проверка и установка MongoDB...${NC}"
if ! command -v mongod >/dev/null 2>&1; then
    echo " -> Добавление официального репозитория MongoDB Community..."
    . /etc/os-release
    DISTRO="${ID:-ubuntu}"
    CODENAME="${VERSION_CODENAME:-noble}"

    # Для Ubuntu 24.04 (noble) и свежих дистрибутивов используется MongoDB 8.0
    MONGO_VER="8.0"
    if [ "$CODENAME" = "focal" ]; then
        MONGO_VER="7.0"
    fi

    curl -fsSL "https://www.mongodb.org/static/pgp/server-${MONGO_VER}.asc" | \
        gpg -o "/usr/share/keyrings/mongodb-server-${MONGO_VER}.gpg" --dearmor --yes 2>/dev/null || true

    if [ "$DISTRO" = "ubuntu" ]; then
        REPO_CODENAME="$CODENAME"
        if [ "$CODENAME" != "noble" ] && [ "$CODENAME" != "jammy" ] && [ "$CODENAME" != "focal" ]; then
            REPO_CODENAME="noble"
        fi
        echo "deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-${MONGO_VER}.gpg ] https://repo.mongodb.org/apt/ubuntu ${REPO_CODENAME}/mongodb-org/${MONGO_VER} multiverse" | \
            tee "/etc/apt/sources.list.d/mongodb-org-${MONGO_VER}.list" >/dev/null
    else
        REPO_CODENAME="$CODENAME"
        if [ "$CODENAME" != "bookworm" ] && [ "$CODENAME" != "bullseye" ]; then
            REPO_CODENAME="bookworm"
        fi
        echo "deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-${MONGO_VER}.gpg ] http://repo.mongodb.org/apt/debian ${REPO_CODENAME}/mongodb-org/${MONGO_VER} main" | \
            tee "/etc/apt/sources.list.d/mongodb-org-${MONGO_VER}.list" >/dev/null
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
echo -e "${CYAN}[3/6] Подготовка рабочих директорий и .env конфигурации...${NC}"
mkdir -p /etc/wdtt /etc/wdtt/backups /usr/local/bin /var/log/wdtt
echo "mongodb://127.0.0.1:27017/wdtt" > /etc/wdtt/mongodb.uri

# Инициализация /etc/wdtt/.env если его нет
if [ ! -f /etc/wdtt/.env ]; then
    echo " -> Создание файла конфигурации /etc/wdtt/.env..."
    if [ -f "${SCRIPT_DIR}/.env" ]; then
        cp "${SCRIPT_DIR}/.env" /etc/wdtt/.env
    elif [ -f "${SCRIPT_DIR}/.env.example" ]; then
        cp "${SCRIPT_DIR}/.env.example" /etc/wdtt/.env
    else
        cat << 'EOF' > /etc/wdtt/.env
MONGODB_URI=mongodb://127.0.0.1:27017/wdtt
MONGODB_DATABASE=wdtt
WDTT_PANEL_PORT=2860
WDTT_PANEL_LISTEN=0.0.0.0
WDTT_DTLS_PORT=56000
WDTT_RAW_PORT=56003
WDTT_SUB_PORT=2096
WDTT_CONFIG_DIR=/etc/wdtt
WDTT_BOT_ENABLED=false
WDTT_BOT_TOKEN=
WDTT_BOT_ADMIN_IDS=
WDTT_BOT_PANEL_URL=http://127.0.0.1:2860/api
WDTT_BOT_API_KEY=
WDTT_BOT_PUBLIC=true
WDTT_BOT_USER_PREFIX=tg_
WDTT_BOT_COPY_NODE_LIMIT=5
WDTT_BOT_DEFAULT_DAYS=30
WDTT_BOT_DEFAULT_TRAFFIC_GB=100
WDTT_BOT_DEFAULT_DEVICES=2
WDTT_BOT_REQUEST_TIMEOUT_SEC=15
WDTT_BOT_TELEGRAM_API=https://api.telegram.org
EOF
    fi
    chmod 600 /etc/wdtt/.env 2>/dev/null || true
fi
if grep -q '^WDTT_INSTALL_MODE=' /etc/wdtt/.env; then
    sed -i "s/^WDTT_INSTALL_MODE=.*/WDTT_INSTALL_MODE=${INSTALL_MODE}/" /etc/wdtt/.env
else
    printf '\nWDTT_INSTALL_MODE=%s\n' "$INSTALL_MODE" >> /etc/wdtt/.env
fi
chmod 600 /etc/wdtt/.env

# 4. Сборка / установка бинарника WDTT
echo -e "${CYAN}[4/6] Сборка и установка бинарника WDTT...${NC}"
cd "$SCRIPT_DIR"

echo " -> Сборка unified бинарника через Go..."
bash ./build.sh "$ARCH" unified

BIN_OUT="wdtt-linux-${ARCH}"
if [ -f "$BIN_OUT" ]; then
    install -m 0755 "$BIN_OUT" /usr/local/bin/wdtt-app
    mkdir -p /etc/wdtt/bin
    cp "$BIN_OUT" /etc/wdtt/bin/wdtt
elif [ -f "/usr/local/bin/wdtt-app" ]; then
    echo " -> Используется существующий бинарник /usr/local/bin/wdtt-app"
    mkdir -p /etc/wdtt/bin
    cp /usr/local/bin/wdtt-app /etc/wdtt/bin/wdtt 2>/dev/null || true
else
    echo -e "${RED}[ERROR] Бинарник $BIN_OUT не найден. Запустите build.sh вручную.${NC}"
    exit 1
fi

# Установка CLI-утилиты и правил маршрутизации
install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-cli.sh" /usr/local/bin/wdtt
printf '%s\n' "$SCRIPT_DIR" > /etc/wdtt/source-dir
if [ -f "${SCRIPT_DIR}/scripts/wdtt-xray-rules.sh" ]; then
    install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-xray-rules.sh" /usr/local/bin/wdtt-xray-rules.sh
fi
if [ -f "${SCRIPT_DIR}/scripts/wdtt-mtu-rules.sh" ]; then
    install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-mtu-rules.sh" /usr/local/bin/wdtt-mtu-rules.sh
fi

cat << 'EOF' > /etc/profile.d/wdtt.sh
alias wdtt='/usr/local/bin/wdtt'
EOF

# 5. Настройка systemd-сервиса
echo -e "${CYAN}[5/6] Создание службы systemd...${NC}"
SERVICE_FLAGS="-config-dir /etc/wdtt"
SERVICE_POST=""
if [ "$INSTALL_MODE" = "panel" ]; then
    SERVICE_FLAGS="$SERVICE_FLAGS -panel-only"
else
    SERVICE_POST="ExecStartPost=-/usr/local/bin/wdtt-xray-rules.sh up"
fi
cat << EOF > /etc/systemd/system/wdtt.service
[Unit]
Description=WDTT Panel (${INSTALL_MODE})
After=network.target
Wants=mongod.service

[Service]
Type=simple
User=root
WorkingDirectory=/etc/wdtt
EnvironmentFile=-/etc/wdtt/.env
ExecStart=/usr/local/bin/wdtt-app ${SERVICE_FLAGS}
${SERVICE_POST}
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
if [ "$INSTALL_MODE" = "panel-node" ] && [ -x /usr/local/bin/wdtt-xray-rules.sh ]; then
    /usr/local/bin/wdtt-xray-rules.sh up || true
fi

# 6. Настройка фаервола
echo -e "${CYAN}[6/6] Настройка фаервола (открытие портов)...${NC}"
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
    ufw allow 2860/tcp comment "WDTT Panel" >/dev/null 2>&1 || true
    ufw allow 2096/tcp comment "WDTT Subscription" >/dev/null 2>&1 || true
    if [ "$INSTALL_MODE" = "panel-node" ]; then
        ufw allow 56000/udp comment "WDTT DTLS" >/dev/null 2>&1 || true
        ufw allow 56003/udp comment "WDTT RAW Direct" >/dev/null 2>&1 || true
    fi
elif command -v iptables >/dev/null 2>&1; then
    iptables -C INPUT -p tcp --dport 2860 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp --dport 2860 -j ACCEPT
    iptables -C INPUT -p tcp --dport 2096 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp --dport 2096 -j ACCEPT
    if [ "$INSTALL_MODE" = "panel-node" ]; then
        iptables -C INPUT -p udp --dport 56000 -j ACCEPT 2>/dev/null || iptables -I INPUT -p udp --dport 56000 -j ACCEPT
        iptables -C INPUT -p udp --dport 56003 -j ACCEPT 2>/dev/null || iptables -I INPUT -p udp --dport 56003 -j ACCEPT
    fi
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
echo -e " Режим установки:     ${BLUE}${INSTALL_MODE}${NC}"
if [ "$INSTALL_MODE" = "panel-node" ]; then
    echo -e " Порты подключения:   ${BLUE}56000/udp${NC} (DTLS) | ${BLUE}56003/udp${NC} (RAW Direct)"
fi
echo -e " База данных:         ${GREEN}MongoDB (mongodb://127.0.0.1:27017/wdtt)${NC}"
echo -e " Управление:          ${BOLD}wdtt${NC} (алиас и интерактивное меню)"
echo ""
echo -e "Пример команд:"
echo -e "  ${BOLD}wdtt${NC}          - Меню управления"
echo -e "  ${BOLD}wdtt status${NC}   - Статус сервера и портов"
echo -e "  ${BOLD}wdtt backup${NC}   - Создание бэкапа базы и настроек"
echo -e "  ${BOLD}wdtt logs${NC}     - Просмотр логов"
echo ""

