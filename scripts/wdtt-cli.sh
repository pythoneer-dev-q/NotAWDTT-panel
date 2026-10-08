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
PANEL_PORT=2860

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
    if systemctl is-active --quiet mongod 2>/dev/null || systemctl is-active --quiet mongodb 2>/dev/null; then
        echo -e "База данных:        ${GREEN}● MongoDB АКТИВНА${NC}"
    else
        echo -e "База данных:        ${YELLOW}○ MongoDB не найдена в systemd (проверьте mongod)${NC}"
    fi

    # 3. Активные порты
    local srv_ip
    srv_ip=$(get_public_ip)
    echo -e "Публичный IP:       ${CYAN}${srv_ip}${NC}"
    echo -e "Панель управления:  ${CYAN}http://${srv_ip}:${PANEL_PORT}/wdtt/${NC}"
    echo -e "Порт DTLS (UDP):    ${BLUE}56000/udp${NC}"
    echo -e "Порт RAW Direct:    ${BLUE}56003/udp${NC}"

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
    read -rp "Введите новый логин панели (default: admin): " new_user
    new_user="${new_user:-admin}"
    read -rsp "Введите новый пароль панели: " new_pass
    echo ""
    if [ -z "$new_pass" ]; then
        echo -e "${RED}Пароль не может быть пустым!${NC}"
        return
    fi

    # Обновление в MongoDB если mongosh установлен
    if command -v mongosh >/dev/null 2>&1; then
        mongosh wdtt --quiet --eval "db.panel_config.updateOne({_id: 1}, {\$set: {username: '$new_user'}})" >/dev/null 2>&1 || true
    fi

    echo -e "${GREEN}✓ Реквизиты входа обновлены!${NC}"
    echo -e "Логин:  ${BOLD}${new_user}${NC}"
    echo -e "Панель: ${CYAN}http://$(get_public_ip):${PANEL_PORT}/wdtt/${NC}"
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
        echo "  0) Выход"
        echo "─────────────────────────────────────────────────"
        read -rp "Введите номер [0-9]: " choice

        case "$choice" in
            1) show_status; read -rp "Нажмите Enter для продолжения..." ;;
            2) restart_service; read -rp "Нажмите Enter для продолжения..." ;;
            3) systemctl stop "$SERVICE_NAME"; echo -e "${YELLOW}Служба остановлена${NC}"; read -rp "Нажмите Enter для продолжения..." ;;
            4) systemctl start "$SERVICE_NAME"; echo -e "${GREEN}Служба запущена${NC}"; read -rp "Нажмите Enter для продолжения..." ;;
            5) show_logs ;;
            6) create_backup; read -rp "Нажмите Enter для продолжения..." ;;
            7) restore_backup ""; read -rp "Нажмите Enter для продолжения..." ;;
            8) reset_admin_password; read -rp "Нажмите Enter для продолжения..." ;;
            9)
                echo -e "\n${BOLD}Вход в веб-панель:${NC}"
                echo -e "URL:    ${CYAN}http://$(get_public_ip):${PANEL_PORT}/wdtt/${NC}"
                echo -e "Логин:  ${BOLD}admin${NC} (по умолчанию)"
                echo -e "Пароль: ${BOLD}wdtt${NC} (по умолчанию)"
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
    info)
        echo -e "Панель: http://$(get_public_ip):${PANEL_PORT}/wdtt/"
        ;;
    help|--help|-h)
        echo "Использование: wdtt [команда]"
        echo "Команды:"
        echo "  wdtt            - Интерактивное меню управления"
        echo "  wdtt status     - Показать текущий статус сервиса и портов"
        echo "  wdtt restart    - Перезапустить службу"
        echo "  wdtt start      - Запустить службу"
        echo "  wdtt stop       - Остановить службу"
        echo "  wdtt backup     - Создать бэкап базы MongoDB и конфигов"
        echo "  wdtt restore    - Восстановить из бэкапа"
        echo "  wdtt logs       - Просмотр логов в реальном времени"
        echo "  wdtt password   - Сбросить пароль администратора"
        ;;
    *)
        interactive_menu
        ;;
esac

