#!/usr/bin/env bash
set -euo pipefail

conf=/etc/wdtt-node/node.conf
if [ "$(id -u)" -ne 0 ]; then echo 'Запустите sudo wdtt-node' >&2; exit 1; fi
if [ ! -f "$conf" ]; then echo "Конфигурация ноды не найдена: $conf" >&2; exit 1; fi
. "$conf"
if [ -n "${HTTPS_PROXY:-}" ]; then export HTTPS_PROXY; fi

node_arch() {
    case "$(uname -m)" in
        x86_64) echo amd64 ;;
        aarch64|arm64) echo arm64 ;;
        *) echo 'Архитектура VPS не поддерживается.' >&2; return 1 ;;
    esac
}

install_node_binary() {
    local source="$1" tmp previous version
    [ -f "$source" ] || { echo 'Файл бинарника не найден.' >&2; return 1; }
    [ "$(od -An -tx1 -N4 "$source" | tr -d ' \n')" = 7f454c46 ] || { echo 'Ожидается ELF-бинарник.' >&2; return 1; }
    tmp=$(mktemp /usr/local/bin/.wdtt-update.XXXXXX)
    previous=$(mktemp /usr/local/bin/.wdtt-backup.XXXXXX)
    if ! install -m 0755 "$source" "$tmp" || ! version=$("$tmp" --version 2>&1); then
        rm -f -- "$tmp" "$previous"
        echo 'Бинарник не запустился на этой ноде.' >&2; return 1
    fi
    echo "Новая версия: $version"
    if ! cp -p /usr/local/bin/wdtt-node-app "$previous"; then
        rm -f -- "$tmp" "$previous"; return 1
    fi
    if ! mv -f -- "$tmp" /usr/local/bin/wdtt-node-app; then
        rm -f -- "$tmp" "$previous"; return 1
    fi
    if systemctl restart wdtt-node.service && sleep 3 && systemctl is-active --quiet wdtt-node.service; then
        rm -f -- "$previous"
        echo 'Нода обновлена. Конфигурация и база сохранены.'
    else
        echo 'Новая версия не запустилась, восстанавливаю прежнюю.' >&2
        mv -f -- "$previous" /usr/local/bin/wdtt-node-app
        systemctl restart wdtt-node.service || echo 'Не удалось запустить прежнюю версию; проверьте journalctl.' >&2
        return 1
    fi
}

case "${1:-}" in
    status)
        systemctl status wdtt-node.service --no-pager
        echo "Панель: $MASTER_URL | Нода: $NODE_ID"
        echo "Версия ноды: $(/usr/local/bin/wdtt-node-app --version 2>/dev/null || echo неизвестна)"
        echo "Коммит ноды: $(/usr/local/bin/wdtt-node-app --build-commit 2>/dev/null || echo неизвестен)"
        echo "UDP: $WDTT_DTLS_PORT | RAW: $WDTT_RAW_PORT"
        ;;
    logs|log) journalctl -u wdtt-node.service -f -n 50 ;;
    restart) systemctl restart wdtt-node.service ;;
    update-file)
        [ "$#" -eq 2 ] || { echo 'Использование: sudo wdtt-node update-file /путь/к/бинарнику' >&2; exit 2; }
        install_node_binary "$2"
        ;;
    update-github)
        [ "$#" -eq 1 ] || { echo 'Использование: sudo wdtt-node update-github' >&2; exit 2; }
        arch=$(node_arch)
        tmp_dir=$(mktemp -d)
        trap 'rm -rf -- "$tmp_dir"' EXIT
        release_url='https://github.com/pythoneer-dev-q/NotAWDTT-panel/releases/latest/download'
        curl --fail --show-error --silent --location --proto '=https' --proto-redir '=https' --retry 2 --connect-timeout 10 --max-time 180 "$release_url/wdtt-linux-$arch" -o "$tmp_dir/wdtt-linux-$arch"
        curl --fail --show-error --silent --location --proto '=https' --proto-redir '=https' --retry 2 --connect-timeout 10 --max-time 60 "$release_url/SHA256SUMS" -o "$tmp_dir/SHA256SUMS"
        (cd "$tmp_dir" && awk -v name="wdtt-linux-$arch" '$2 == name {print}' SHA256SUMS | sha256sum --check --status) || { echo 'Контрольная сумма не совпала.' >&2; exit 1; }
        install_node_binary "$tmp_dir/wdtt-linux-$arch"
        ;;
    reinstall|update)
        curl_opts=(--fail --show-error --silent --proto '=https' --max-redirs 0 --retry 2 --connect-timeout 10 --max-time 90)
        if [ -n "${NODE_CERT_PIN:-}" ]; then curl_opts+=(--insecure --pinnedpubkey "$NODE_CERT_PIN"); fi
        tmp=$(mktemp)
        trap 'rm -f -- "$tmp"' EXIT
        if curl --fail --show-error --silent --proto '=https' --max-redirs 0 --retry 2 --connect-timeout 10 --max-time 60 \
            https://raw.githubusercontent.com/pythoneer-dev-q/NotAWDTT-panel/main/node-install.sh -o "$tmp" \
            && [ -s "$tmp" ] && [ "$(head -c 19 "$tmp")" = '#!/usr/bin/env bash' ]; then
            args=(--panel "${MASTER_URL%/panel/api}")
            if [ -n "${NODE_CERT_PIN:-}" ]; then args+=(--pin "$NODE_CERT_PIN"); fi
            WDTT_NODE_SECRET="$NODE_TOKEN" bash "$tmp" "${args[@]}"
        else
            echo 'GitHub недоступен с VPS. Загружаю установщик напрямую с панели.' >&2
            curl "${curl_opts[@]}" -H "Authorization: Bearer $NODE_TOKEN" "$MASTER_URL/node/install?id=$NODE_ID" -o "$tmp"
            bash "$tmp"
        fi
        ;;
    uninstall)
        read -rp 'Введите УДАЛИТЬ для удаления службы и бинарника ноды: ' answer
        [ "$answer" = 'УДАЛИТЬ' ] || { echo 'Отмена.'; exit 0; }
        systemctl disable --now wdtt-node.service
        rm -f -- /etc/systemd/system/wdtt-node.service /usr/local/bin/wdtt-node-app /usr/local/bin/wdtt-node /usr/local/libexec/wdtt-node-firewall
        systemctl daemon-reload
        echo 'Служба удалена. Конфигурация и ключ остались в /etc/wdtt-node/node.conf.'
        ;;
    ''|help|--help|-h)
        echo 'wdtt-node status | logs | restart | reinstall | update-github | update-file ПУТЬ | uninstall'
        ;;
    *) echo "Неизвестная команда: $1" >&2; exit 2 ;;
esac
