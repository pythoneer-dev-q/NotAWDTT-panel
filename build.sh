#!/bin/bash
# Сборка wdtt-server, wdtt-panel и/или единого wdtt для Linux
set -euo pipefail
cd "$(dirname "$0")"

export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    return 0
  fi
  echo "═══════════════════════════════════════════════════════"
  echo "    Go не найден. Автоматическая установка Go..."
  echo "═══════════════════════════════════════════════════════"
  local arch="amd64"
  case "$(uname -m)" in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
  esac
  local go_ver="1.25.14"
  local tarball="go${go_ver}.linux-${arch}.tar.gz"
  echo " -> Загрузка Go ${go_ver} (${arch})..."
  mkdir -p /tmp
  if ! curl -fsSL "https://go.dev/dl/${tarball}" -o "/tmp/${tarball}"; then
    curl -fsSL "https://golang.org/dl/${tarball}" -o "/tmp/${tarball}"
  fi
  echo " -> Распаковка в /usr/local/go..."
  rm -rf /usr/local/go && tar -C /usr/local -xzf "/tmp/${tarball}"
  rm -f "/tmp/${tarball}"
  export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"

  if [ -f /etc/profile ] && ! grep -q '/usr/local/go/bin' /etc/profile; then
    echo 'export PATH=/usr/local/go/bin:$PATH' >> /etc/profile
  fi
  if [ -f /root/.bashrc ] && ! grep -q '/usr/local/go/bin' /root/.bashrc; then
    echo 'export PATH=/usr/local/go/bin:$PATH' >> /root/.bashrc
  fi

  echo "✓ Go $(go version) успешно установлен!"
}

ensure_go

ARCH="${1:-amd64}"
TARGET="${2:-all}"

case "$ARCH" in
  amd64|arm64) ;;
  server|panel|all|unified)
    TARGET="$ARCH"
    ARCH=amd64
    ;;
  *)
    echo "Usage: $0 [amd64|arm64] [server|panel|all|unified]"
    exit 1
    ;;
esac

case "$TARGET" in
  server|panel|all|unified) ;;
  *)
    echo "Usage: $0 [amd64|arm64] [server|panel|all|unified]"
    exit 1
    ;;
esac

build_server() {
  local goarch="$1"
  local out
  case "$goarch" in
    amd64) out=wdtt-server-linux-amd64 ;;
    arm64) out=wdtt-server-linux-arm64 ;;
  esac
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out" ./server/cmd
  chmod +x "$out"
  echo "OK: $out"
}

build_panel() {
  chmod +x scripts/bundle-panel-core.sh panel/build.sh
  scripts/bundle-panel-core.sh
  panel/build.sh "$1"
}

build_unified() {
  local goarch="$1"
  local out
  case "$goarch" in
    amd64) out=wdtt-linux-amd64 ;;
    arm64) out=wdtt-linux-arm64 ;;
  esac
  chmod +x scripts/bundle-panel-core.sh
  if [[ "${WDTT_SKIP_BUNDLE:-}" == "1" && -f panel/web/assets/js/panel-core.min.js ]]; then
    echo "SKIP: panel-core.min.js (WDTT_SKIP_BUNDLE=1)"
  else
    scripts/bundle-panel-core.sh
  fi
  local version="${WDTT_VERSION:-}"
  if [[ -z "$version" ]]; then
    version="$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo 1.4.0)"
  fi
  local commit
  commit="$(git rev-parse HEAD 2>/dev/null || true)"
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w -X wdtt-panel.panelVersion=${version} -X wdtt-panel.panelBuildCommit=${commit}" -o "$out" ./cmd/wdtt
  chmod +x "$out"
  echo "OK: $out (server + panel, v${version})"
}

case "$TARGET" in
  server)  build_server "$ARCH" ;;
  panel)   build_panel "$ARCH" ;;
  unified) build_unified "$ARCH" ;;
  all)
    build_server "$ARCH"
    build_panel "$ARCH"
    build_unified "$ARCH"
    ;;
esac
