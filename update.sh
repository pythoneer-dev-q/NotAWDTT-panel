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
UPDATE_REPO="${WDTT_UPDATE_REPO:-https://github.com/pythoneer-dev-q/NotAWDTT-panel.git}"

if [ "${1:-}" = "--check" ]; then
    current_commit="$(git rev-parse HEAD)"
    latest_commit="$(GIT_TERMINAL_PROMPT=0 timeout 20 git ls-remote "$UPDATE_REPO" refs/heads/main | awk '$2 == "refs/heads/main" { print $1 }')"
    [ -n "$latest_commit" ] || { echo 'Не удалось получить версию main с GitHub' >&2; exit 1; }
    installed_commit="$(/usr/local/bin/wdtt-app --build-commit 2>/dev/null || true)"
    printf 'Исходники: %s\nGitHub main: %s\nУстановленный бинарник: %s\n' "$current_commit" "$latest_commit" "${installed_commit:-версия неизвестна}"
    if [ "$current_commit" = "$latest_commit" ] && [ "$installed_commit" = "$current_commit" ]; then
        echo 'Панель актуальна.'
    else
        echo 'Доступно обновление или требуется пересборка бинарника.'
    fi
    exit 0
fi

# Keep the selected verbosity across subsequent updates and restarts.
SAVED_LOG_LEVEL=""
if [ -f /etc/wdtt/.env ]; then
    SAVED_LOG_LEVEL=$(sed -n 's/^WDTT_LOG_LEVEL=//p' /etc/wdtt/.env | tail -n 1)
fi
LOG_LEVEL="${WDTT_LOG_LEVEL:-${SAVED_LOG_LEVEL:-info}}"
if [ "${1:-}" = "--log-level" ]; then
    LOG_LEVEL="${2:-}"
elif [ -t 0 ]; then
    read -rp "Уровень логов [debug/info/warn/error] (Enter: ${LOG_LEVEL}): " selected_level
    LOG_LEVEL="${selected_level:-$LOG_LEVEL}"
fi
case "$LOG_LEVEL" in
    debug|info|warn|error) ;;
    *) echo "Некорректный уровень логов: $LOG_LEVEL" >&2; exit 1 ;;
esac
mkdir -p /etc/wdtt /etc/systemd/system/wdtt.service.d
touch /etc/wdtt/.env
if grep -q '^WDTT_LOG_LEVEL=' /etc/wdtt/.env; then
    sed -i "s/^WDTT_LOG_LEVEL=.*/WDTT_LOG_LEVEL=${LOG_LEVEL}/" /etc/wdtt/.env
else
    printf '\nWDTT_LOG_LEVEL=%s\n' "$LOG_LEVEL" >> /etc/wdtt/.env
fi
chmod 600 /etc/wdtt/.env
printf '[Service]\nEnvironmentFile=-/etc/wdtt/.env\n' > /etc/systemd/system/wdtt.service.d/10-env.conf

echo -e "${CYAN}${BOLD}"
echo "═══════════════════════════════════════════════════════"
echo "    Быстрое обновление WDTT из Git-репозитория"
echo "═══════════════════════════════════════════════════════"
echo -e "${NC}"

echo -e "${CYAN}[1/4] Загрузка последних изменений из репозитория...${NC}"
GIT_TERMINAL_PROMPT=0 git fetch "$UPDATE_REPO" refs/heads/main
if ! git merge-base --is-ancestor HEAD FETCH_HEAD; then
    echo 'Текущая ветка расходится с main; автоматическое обновление остановлено.' >&2
    exit 1
fi
git merge --ff-only FETCH_HEAD

echo -e "${CYAN}[2/4] Сборка единого бинарника WDTT (Unified RAW/UDP + Panel)...${NC}"
ARCH="amd64"
case "$(uname -m)" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Архитектура $(uname -m) не поддерживается"; exit 1 ;;
esac

bash ./build.sh "$ARCH" unified

OTHER_ARCH=arm64
[ "$ARCH" = arm64 ] && OTHER_ARCH=amd64
if [ -f "/etc/wdtt/bin/wdtt-linux-${OTHER_ARCH}" ]; then
    echo "Обновление бинарника для нод ${OTHER_ARCH}..."
    bash ./build.sh "$OTHER_ARCH" unified
    install -m 0755 "wdtt-linux-${OTHER_ARCH}" "/etc/wdtt/bin/wdtt-linux-${OTHER_ARCH}"
fi

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
install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-mtu-rules.sh" /usr/local/bin/wdtt-mtu-rules.sh
install -m 0755 "${SCRIPT_DIR}/scripts/wdtt-xray-rules.sh" /usr/local/bin/wdtt-xray-rules.sh
printf '%s\n' "$SCRIPT_DIR" > /etc/wdtt/source-dir

echo -e "${CYAN}[4/4] Перезапуск службы wdtt...${NC}"
systemctl daemon-reload
systemctl restart wdtt.service
sleep 2

echo ""
echo -e "${GREEN}${BOLD}✓ Панель и сервер WDTT успешно обновлены!${NC}"
echo ""
/usr/local/bin/wdtt status || true
