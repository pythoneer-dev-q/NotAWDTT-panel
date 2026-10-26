#!/usr/bin/env bash
set -euo pipefail

conf=/etc/wdtt-node/node.conf
if [ "$(id -u)" -ne 0 ]; then echo 'Запустите sudo wdtt-node' >&2; exit 1; fi
if [ ! -f "$conf" ]; then echo "Конфигурация ноды не найдена: $conf" >&2; exit 1; fi
. "$conf"
if [ -n "${HTTPS_PROXY:-}" ]; then export HTTPS_PROXY; fi

case "${1:-}" in
    status)
        systemctl status wdtt-node.service --no-pager
        echo "Панель: $MASTER_URL | Нода: $NODE_ID"
        echo "UDP: $WDTT_DTLS_PORT | RAW: $WDTT_RAW_PORT"
        ;;
    logs|log) journalctl -u wdtt-node.service -f -n 50 ;;
    restart) systemctl restart wdtt-node.service ;;
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
        echo 'wdtt-node status | logs | restart | reinstall | uninstall'
        ;;
    *) echo "Неизвестная команда: $1" >&2; exit 2 ;;
esac
