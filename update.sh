#!/usr/bin/env bash
# ==============================================================================
# WDTT Quick Update Script — Быстрое обновление панели и VPN-ядра из Git
# ==============================================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

if [ "$(id -u)" -ne 0 ]; then
    echo -e "${RED}[ERROR] Скрипт должен быть запущен с правами root: sudo bash update.sh${NC}"
    exit 1
fi

export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${CYAN}${BOLD}"
echo "═══════════════════════════════════════════════════════"
echo "    Быстрое обновление WDTT из Git-репозитория"
echo "═══════════════════════════════════════════════════════"
echo -e "${NC}"

echo -e "${CYAN}[1/4] Загрузка последних изменений из репозитория...${NC}"
git pull

echo -e "${CYAN}[2/4] Сборка единого бинарника WDTT (Unified RAW/UDP + Panel)...${NC}"
ARCH="amd64"
case "$(uname -m)" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Архитектура $(uname -m) не поддерживается"; exit 1 ;;
esac

bash ./build.sh "$ARCH" unified

BIN_OUT="wdtt-linux-${ARCH}"
if [ ! -f "$BIN_OUT" ]; then
    echo -e "${RED}[ERROR] Бинарник $BIN_OUT не собрался${NC}"
    exit 1
fi

echo -e "${CYAN}[3/4] Установка обновлённых файлов...${NC}"
install -m 0755 "$BIN_OUT" /usr/local/bin/wdtt-app
mkdir -p /etc/wdtt/bin
cp "$BIN_OUT" /etc/wdtt/bin/wdtt
install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-cli.sh" /usr/local/bin/wdtt

echo -e "${CYAN}[4/4] Перезапуск службы wdtt...${NC}"
systemctl daemon-reload
systemctl restart wdtt.service
sleep 2

echo ""
echo -e "${GREEN}${BOLD}✓ Панель и сервер WDTT успешно обновлены!${NC}"
echo ""
/usr/local/bin/wdtt status || true
