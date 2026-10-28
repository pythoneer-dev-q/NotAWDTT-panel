#!/usr/bin/env bash
# ==============================================================================
# WDTT CLI — Утилита управления сервером и панелью WDTT
# ==============================================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

SERVICE_NAME="wdtt"
CONFIG_DIR="/etc/wdtt"
BACKUP_DIR="/etc/wdtt/backups"
MONGO_URI_FILE="/etc/wdtt/mongodb.uri"
ENV_FILE="/etc/wdtt/.env"

if [ -f "$ENV_FILE" ]; then
    set -a
    . "$ENV_FILE" 2>/dev/null || true
    set +a
fi

PANEL_PORT="${WDTT_PANEL_PORT:-2860}"
DTLS_PORT="${WDTT_DTLS_PORT:-56000}"
RAW_PORT="${WDTT_RAW_PORT:-56003}"

function print_banner() {
    clear 2>/dev/null || true
    echo -e "${CYAN}${BOLD}"
    echo "  ██╗    ██╗██████╗ ████████╗████████╗"
    echo "  ██║    ██║██╔══██╗╚══██╔══╝╚══██╔══╝"
    echo "  ██║ █╗ ██║██║  ██║   ██║      ██║   "
    echo "  ██║███╗██║██║  ██║   ██║      ██║   "
    echo "  ╚███╔███╔╝██████╔╝   ██║      ██║   "
    echo "   ╚══╝╚══╝ ╚═════╝    ╚═╝      ╚═╝   "
    echo -e "      ${GREEN}RAW & UDP Anti-Censorship VPN${NC}"
    echo "═════════════════════════════════════════════════"
}

function check_root() {
    if [ "$EUID" -ne 0 ]; then
        echo -e "${RED}[ERROR] Запустите скрипт с правами root (sudo wdtt)${NC}"
        exit 1
    fi
}

function get_public_ip() {
    local ip
    ip=$(curl -4 -s --max-time 3 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')
    echo "${ip:-127.0.0.1}"
}

function show_status() {
    echo -e "\n${BOLD}=== СТАТУС WDTT ===${NC}"
    
    # 1. Служба WDTT
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "Служба WDTT:        ${GREEN}● РАБОТАЕТ${NC} (active)"
    else
        echo -e "Служба WDTT:        ${RED}○ ОСТАНОВЛЕНА${NC} (inactive)"
    fi

    # 2. MongoDB
    local mongo_uri="${MONGODB_URI:-mongodb://127.0.0.1:27017/wdtt}"
    local safe_mongo_uri
    safe_mongo_uri=$(printf '%s' "$mongo_uri" | sed -E 's#(mongodb(\+srv)?://)[^/@]+:[^/@]+@#\1***:***@#')
    echo -e "MongoDB URI:        ${CYAN}${safe_mongo_uri}${NC}"
    if systemctl is-active --quiet mongod 2>/dev/null || systemctl is-active --quiet mongodb 2>/dev/null; then
        echo -e "База данных:        ${GREEN}● mongod активна${NC}"
    elif [[ "$mongo_uri" != *"127.0.0.1"* && "$mongo_uri" != *"localhost"* ]]; then
        echo -e "База данных:        ${GREEN}● Удалённый кластер / MongoDB Atlas${NC}"
    else
        echo -e "База данных:        ${YELLOW}○ Локальная mongod не запущена в systemd${NC}"
    fi

    # 3. Активные порты
    local srv_ip
    srv_ip=$(get_public_ip)
    echo -e "Публичный IP:       ${CYAN}${srv_ip}${NC}"
    echo -e "Панель управления:  ${CYAN}http://${srv_ip}:${PANEL_PORT}/wdtt/${NC}"
    echo -e "Порт DTLS (UDP):    ${BLUE}${DTLS_PORT}/udp${NC}"
    echo -e "Порт RAW Direct:    ${BLUE}${RAW_PORT}/udp${NC}"

    # 4. TUN-интерфейс
    if ip link show wdtt-raw >/dev/null 2>&1; then
        echo -e "TUN-интерфейс:      ${GREEN}wdtt-raw (UP)${NC}"
    else
        echo -e "TUN-интерфейс:      ${YELLOW}wdtt-raw (поднимется при первом клиенте)${NC}"
    fi
    echo ""
}

function create_backup() {
    mkdir -p "$BACKUP_DIR"
    local timestamp
    timestamp=$(date +"%Y%m%d_%H%M%S")
    local backup_file="${BACKUP_DIR}/wdtt_backup_${timestamp}.tar.gz"
    local tmp_dir
    tmp_dir=$(mktemp -d)

    echo -e "${CYAN}[BACKUP] Создание резервной копии...${NC}"

    # Дамп MongoDB если доступен mongodump
    if command -v mongodump >/dev/null 2>&1; then
        echo " -> Дамп коллекций MongoDB (база: wdtt)..."
        mongodump --db=wdtt --out="${tmp_dir}/mongo_dump" --quiet 2>/dev/null || true
    fi

    # Копирование конфигов
    mkdir -p "${tmp_dir}/configs"
    if [ -d "$CONFIG_DIR" ]; then
        cp -r "$CONFIG_DIR"/* "${tmp_dir}/configs/" 2>/dev/null || true
        # Исключаем сами бэкапы
        rm -rf "${tmp_dir}/configs/backups"
    fi

    # Упаковка в tar.gz
    tar -czf "$backup_file" -C "$tmp_dir" .
    rm -rf "$tmp_dir"

    echo -e "${GREEN}✓ Резервная копия успешно создана:${NC} ${BOLD}${backup_file}${NC}"
    echo -e "Размер: $(du -h "$backup_file" | awk '{print $1}')"
}

function restore_backup() {
    local file="$1"
    if [ -z "$file" ]; then
        echo -e "${YELLOW}Доступные бэкапы:${NC}"
        ls -lh "$BACKUP_DIR"/*.tar.gz 2>/dev/null || {
            echo "Нет доступных резервных копий в $BACKUP_DIR"
            return
        }
        echo ""
        read -rp "Введите полный путь к файлу бэкапа: " file
    fi

    if [ ! -f "$file" ]; then
        echo -e "${RED}[ERROR] Файл не найден: $file${NC}"
        return
    fi

    echo -e "${YELLOW}ВНИМАНИЕ: Восстановление перезапишет текущую базу и конфиги!${NC}"
    read -rp "Продолжить? (y/N): " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        echo "Отмена."
        return
    fi

    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    local tmp_dir
    tmp_dir=$(mktemp -d)
    tar -xzf "$file" -C "$tmp_dir"

    # Восстановление MongoDB
    if [ -d "${tmp_dir}/mongo_dump" ] && command -v mongorestore >/dev/null 2>&1; then
        echo " -> Восстановление MongoDB..."
        mongorestore --drop --dir="${tmp_dir}/mongo_dump" --quiet 2>/dev/null || true
    fi

    # Восстановление конфигов
    if [ -d "${tmp_dir}/configs" ]; then
        echo " -> Восстановление конфигурационных файлов..."
        cp -r "${tmp_dir}/configs"/* "$CONFIG_DIR/" 2>/dev/null || true
    fi

    rm -rf "$tmp_dir"
    systemctl start "$SERVICE_NAME" 2>/dev/null || true

    echo -e "${GREEN}✓ Восстановление успешно завершено! Служба перезапущена.${NC}"
}

function show_logs() {
    echo -e "${CYAN}Нажмите Ctrl+C для выхода из логов.${NC}\n"
    journalctl -u "$SERVICE_NAME" -f -n 50 --no-tail
}

function restart_service() {
    echo -e "${CYAN}Перезапуск службы WDTT...${NC}"
    systemctl restart "$SERVICE_NAME"
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "${GREEN}✓ Служба успешно перезапущена!${NC}"
    else
        echo -e "${RED}✗ Ошибка запуска службы! Проверьте логи:${NC} journalctl -u wdtt -n 20"
    fi
}

function reset_admin_password() {
    echo -e "\n${BOLD}=== СБРОС ПАРОЛЯ АДМИНИСТРАТОРА ПАНЕЛИ ===${NC}"
    read -rp "Новый логин панели (Enter — оставить текущий): " new_user
    read -rsp "Введите новый пароль панели: " new_pass
    echo ""
    if [ -z "$new_pass" ]; then
        echo -e "${RED}Пароль не может быть пустым!${NC}"
        return
    fi
    local confirm_pass
    read -rsp "Повторите пароль: " confirm_pass
    echo ""
    if [ "$new_pass" != "$confirm_pass" ]; then
        echo -e "${RED}Пароли не совпадают${NC}"
        return 1
    fi

    local app_bin="/usr/local/bin/wdtt-app"
    if [ ! -x "$app_bin" ]; then
        echo -e "${RED}Бинарник панели не найден: $app_bin${NC}"
        return 1
    fi
    if ! printf '%s\n' "$new_pass" | "$app_bin" -reset-admin "$new_user"; then
        echo -e "${RED}Не удалось обновить реквизиты входа${NC}"
        return 1
    fi
    systemctl restart "$SERVICE_NAME"

    echo -e "${GREEN}✓ Реквизиты входа обновлены!${NC}"
    if [ -n "$new_user" ]; then
        echo -e "Логин:  ${BOLD}${new_user}${NC}"
    else
        echo "Логин оставлен прежним"
    fi
    echo -e "Панель: ${CYAN}http://$(get_public_ip):${PANEL_PORT}/wdtt/${NC}"
}

function edit_env() {
    local env_file="/etc/wdtt/.env"
    if [ ! -f "$env_file" ]; then
        if [ -f "$MONGO_URI_FILE" ]; then
            echo "MONGODB_URI=$(cat "$MONGO_URI_FILE")" > "$env_file"
        else
            echo "MONGODB_URI=mongodb://127.0.0.1:27017/wdtt" > "$env_file"
        fi
    fi
    local editor="${EDITOR:-nano}"
    if ! command -v "$editor" >/dev/null 2>&1; then
        editor="vi"
    fi
    "$editor" "$env_file"
    echo ""
    read -rp "Перезапустить службу WDTT для применения настроек? [Y/n]: " ans
    if [[ "$ans" =~ ^[Yy]$ || "$ans" == "" ]]; then
        restart_service
    fi
}

function reinstall_wdtt() {
    local mode_flag="--panel-node"
    if [ "${WDTT_INSTALL_MODE:-}" = "panel" ]; then
        mode_flag="--panel-only"
    fi
    echo -e "${YELLOW}Переустановка WDTT. Конфигурация и MongoDB сохранятся.${NC}"
    read -rp "Продолжить? [y/N]: " confirm
    [[ "$confirm" =~ ^[Yy]$ ]] || { echo "Отмена."; return 0; }

    local source_dir="${WDTT_INSTALL_SOURCE_DIR:-}"
    if [ -z "$source_dir" ] && [ -f /etc/wdtt/source-dir ]; then
        IFS= read -r source_dir < /etc/wdtt/source-dir
    fi
    source_dir="${source_dir:-/usr/local/src/wdtt-panel-install}"
    if [ -f "$source_dir/setup.sh" ]; then
        if [ -d "$source_dir/.git" ]; then
            git -C "$source_dir" pull --ff-only || return 1
        fi
        bash "$source_dir/setup.sh" "$mode_flag"
        return
    fi
    local bootstrap
    bootstrap=$(mktemp) || return 1
    if ! curl -fsSL --retry 2 'https://raw.githubusercontent.com/pythoneer-dev-q/NotAWDTT-panel/main/install.sh' -o "$bootstrap"; then
        rm -f -- "$bootstrap"
        echo -e "${RED}Не удалось загрузить установщик. Укажите WDTT_INSTALL_SOURCE_DIR с локальным репозиторием.${NC}" >&2
        return 1
    fi
    bash "$bootstrap" "$mode_flag"
    local result=$?
    rm -f -- "$bootstrap"
    return "$result"
}

function uninstall_wdtt() {
    echo -e "${YELLOW}Будут удалены служба WDTT, её бинарник и команда wdtt.${NC}"
    echo "Конфигурация /etc/wdtt, резервные копии и MongoDB сохранятся."
    read -rp "Введите УДАЛИТЬ для подтверждения: " confirm
    [ "$confirm" = "УДАЛИТЬ" ] || { echo "Отмена."; return 0; }

    systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
    if [ -x /usr/local/bin/wdtt-xray-rules.sh ]; then
        /usr/local/bin/wdtt-xray-rules.sh down || true
    fi
    if [ -x /usr/local/bin/wdtt-mtu-rules.sh ]; then
        /usr/local/bin/wdtt-mtu-rules.sh down || true
    fi
    rm -f -- /etc/systemd/system/wdtt.service /etc/systemd/system/wdtt.service.d/10-env.conf
    rm -f -- /usr/local/bin/wdtt-app /usr/local/bin/wdtt /usr/local/bin/wdtt-xray-rules.sh /usr/local/bin/wdtt-mtu-rules.sh /etc/profile.d/wdtt.sh
    systemctl daemon-reload
    echo -e "${GREEN}WDTT удалён. Данные остались в /etc/wdtt и MongoDB.${NC}"
}

function update_wdtt() {
    local source_dir="${WDTT_INSTALL_SOURCE_DIR:-}"
    if [ -z "$source_dir" ] && [ -f /etc/wdtt/source-dir ]; then
        IFS= read -r source_dir < /etc/wdtt/source-dir
    fi
    source_dir="${source_dir:-/usr/local/src/wdtt-panel-install}"
    if [ ! -f "$source_dir/update.sh" ] || [ ! -d "$source_dir/.git" ]; then
        echo "Исходный Git-репозиторий не найден: $source_dir. Обновление не выполнено." >&2
        return 1
    fi
    bash "$source_dir/update.sh" "$@"
}

function interactive_menu() {
    check_root
    while true; do
        print_banner
        echo -e "${BOLD}Выберите действие:${NC}"
        echo "  1) Статус службы и портов"
        echo "  2) Перезапустить службу WDTT"
        echo "  3) Остановить службу WDTT"
        echo "  4) Запустить службу WDTT"
        echo "  5) Просмотр логов в реальном времени"
        echo "  6) Создать резервную копию (Backup)"
        echo "  7) Восстановить из резервной копии (Restore)"
        echo "  8) Сменить пароль администратора панели"
        echo "  9) Информация для входа в панель"
        echo "  10) Редактировать конфигурацию .env (MongoDB, порты)"
        echo "  11) Переустановить WDTT"
        echo "  12) Удалить WDTT (с сохранением данных)"
        echo "  13) Обновить WDTT из репозитория"
        echo "  0) Выход"
        echo "─────────────────────────────────────────────────"
        read -rp "Введите номер [0-12]: " choice

        case "$choice" in
            1) show_status; read -rp "Нажмите Enter для продолжения..." ;;
            2) restart_service; read -rp "Нажмите Enter для продолжения..." ;;
            3) systemctl stop "$SERVICE_NAME"; echo -e "${YELLOW}Служба остановлена${NC}"; read -rp "Нажмите Enter для продолжения..." ;;
            4) systemctl start "$SERVICE_NAME"; echo -e "${GREEN}Служба запущена${NC}"; read -rp "Нажмите Enter для продолжения..." ;;
            5) show_logs ;;
            6) create_backup; read -rp "Нажмите Enter для продолжения..." ;;
            7) restore_backup ""; read -rp "Нажмите Enter для продолжения..." ;;
            8) reset_admin_password; read -rp "Нажмите Enter для продолжения..." ;;
            10) edit_env ;;
            11) reinstall_wdtt; read -rp "Нажмите Enter для продолжения..." ;;
            12) uninstall_wdtt; return ;;
            13) update_wdtt; read -rp "Нажмите Enter для продолжения..." ;;
            9)
                echo -e "\n${BOLD}Вход в веб-панель:${NC}"
                echo -e "URL:    ${CYAN}http://$(get_public_ip):${PANEL_PORT}/wdtt/${NC}"
                echo "Текущие реквизиты не отображаются. Для смены используйте wdtt password."
                read -rp "Нажмите Enter для продолжения..."
                ;;
            0) exit 0 ;;
            *) echo -e "${RED}Неверный ввод!${NC}"; sleep 1 ;;
        esac
    done
}

# Обработка аргументов CLI
check_root

case "${1:-}" in
    status)
        show_status
        ;;
    restart)
        restart_service
        ;;
    start)
        systemctl start "$SERVICE_NAME"
        echo -e "${GREEN}✓ Служба запущена${NC}"
        ;;
    stop)
        systemctl stop "$SERVICE_NAME"
        echo -e "${YELLOW}✓ Служба остановлена${NC}"
        ;;
    backup)
        create_backup
        ;;
    restore)
        restore_backup "${2:-}"
        ;;
    log|logs)
        show_logs
        ;;
    pass|password)
        reset_admin_password
        ;;
    env|config)
        edit_env
        ;;
    reinstall)
        reinstall_wdtt
        ;;
    update)
        shift
        update_wdtt "$@"
        ;;
    uninstall|remove)
        uninstall_wdtt
        ;;
    info)
        echo -e "Панель: http://$(get_public_ip):${PANEL_PORT}/wdtt/"
        ;;
    help|--help|-h)
        echo "Использование: wdtt [команда]"
        echo "Команды:"
        echo "  wdtt            - Интерактивное меню управления"
        echo "  wdtt status     - Показать текущий статус сервиса и портов"
        echo "  wdtt env        - Редактировать настройки .env (MongoDB URI, порты)"
        echo "  wdtt restart    - Перезапустить службу"
        echo "  wdtt start      - Запустить службу"
        echo "  wdtt stop       - Остановить службу"
        echo "  wdtt backup     - Создать бэкап базы MongoDB и конфигов"
        echo "  wdtt restore    - Восстановить из бэкапа"
        echo "  wdtt logs       - Просмотр логов в реальном времени"
        echo "  wdtt password   - Сбросить пароль администратора"
        echo "  wdtt update     - Обновить панель и VPN из репозитория"
        echo "  wdtt reinstall  - Переустановить WDTT с сохранением данных"
        echo "  wdtt uninstall  - Удалить службу и бинарники с сохранением данных"
        ;;
    *)
        interactive_menu
        ;;
esac

