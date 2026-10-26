#!/usr/bin/env bash
# Online bootstrap for the installer maintained in this repository.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "Запустите установщик под root: sudo bash install.sh" >&2
    exit 1
fi

repo="${WDTT_REPO:-https://github.com/pythoneer-dev-q/NotAWDTT-panel.git}"
checkout="${WDTT_INSTALL_SOURCE_DIR:-/usr/local/src/wdtt-panel-install}"
if ! command -v git >/dev/null 2>&1; then
    command -v apt-get >/dev/null 2>&1 || { echo "Для онлайн-установки нужен git" >&2; exit 1; }
    apt-get update -y
    apt-get install -y git
fi

if [ -e "$checkout" ]; then
    [ -d "$checkout/.git" ] || { echo "Каталог $checkout уже занят" >&2; exit 1; }
    origin="$(git -C "$checkout" remote get-url origin)"
    [ "$origin" = "$repo" ] || { echo "Источник $checkout отличается от $repo" >&2; exit 1; }
    git -C "$checkout" pull --ff-only
else
    git clone --depth 1 --branch main "$repo" "$checkout"
fi

bash "$checkout/setup.sh" "$@"
