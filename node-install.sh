#!/usr/bin/env bash
# Public GitHub bootstrap. The node API key is entered on the VPS and sent only
# to the operator's panel over HTTPS.
set -Eeuo pipefail

usage() {
    echo 'Использование: bash node-install.sh --panel https://panel.example/wdtt/ [--pin sha256//...]'
}

panel_url=''
cert_pin=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        --panel)
            [ "$#" -ge 2 ] || { usage >&2; exit 2; }
            panel_url="$2"
            shift 2
            ;;
        --pin)
            [ "$#" -ge 2 ] || { usage >&2; exit 2; }
            cert_pin="$2"
            shift 2
            ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo 'Запустите установщик от root или через sudo.' >&2
    exit 1
fi
if [[ ! "$panel_url" =~ ^https://[^/?#@[:space:]]+(/[A-Za-z0-9/_-]*)?/?$ ]]; then
    echo 'Укажите HTTPS-адрес панели из раздела «Ноды».' >&2
    exit 2
fi
if [ -n "$cert_pin" ] && [[ ! "$cert_pin" =~ ^sha256//[A-Za-z0-9+/]{43}=$ ]]; then
    echo 'Неверный отпечаток публичного ключа сертификата.' >&2
    exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
    if ! command -v apt-get >/dev/null 2>&1; then
        echo 'Для загрузки нужен curl.' >&2
        exit 1
    fi
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y curl ca-certificates
fi

if [ -z "${WDTT_NODE_SECRET:-}" ]; then
    if [ ! -r /dev/tty ]; then
        echo 'Нет терминала для ввода секрета. Запустите команду в SSH-терминале.' >&2
        exit 1
    fi
    read -rsp 'Секрет ноды из панели: ' WDTT_NODE_SECRET < /dev/tty
    echo >&2
fi
if [[ ! "$WDTT_NODE_SECRET" =~ ^[A-Za-z0-9_-]{16,128}$ ]]; then
    echo 'Неверный формат секрета ноды.' >&2
    exit 2
fi

umask 077
installer=$(mktemp)
trap 'rm -f -- "$installer"' EXIT
panel_url="${panel_url%/}"
curl_args=(--fail --show-error --silent --proto '=https' --tlsv1.2 --max-redirs 0 --connect-timeout 10 --max-time 60)
if [ -n "$cert_pin" ]; then
    curl_args+=(--insecure --pinnedpubkey "$cert_pin")
fi
echo 'Проверка секрета и загрузка персонального установщика с панели...'
curl "${curl_args[@]}" -H "Authorization: Bearer $WDTT_NODE_SECRET" \
    "$panel_url/panel/api/node/install" -o "$installer"
unset WDTT_NODE_SECRET
bash "$installer"
