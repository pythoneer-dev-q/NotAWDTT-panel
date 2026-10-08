# WDTT

[![Поддержать WDTT](https://devgamemaga.mooo.com:9443/b/soft.svg)](https://devgamemaga.mooo.com:9443/donate)

VPN на VPS через медиарелеи ВК (TURN / RAW UDP) с современной веб-панелью в стиле **Remnawave**, поддержкой **MongoDB**, многонодовых кластеров и полноценным **RESTful API**.

```
wdtt/
├── cmd/wdtt/                           # Единый бинарник (сервер + панель + подписки)
├── server/                             # wdtt-server (DTLS + RAW UDP + userspace WG + NAT)
├── panel/                              # wdtt-panel (современный Remnawave UI + REST API)
├── pkg/                                # sharelink, vkhash, paneldb (MongoDB + SQLite)
├── scripts/                            # wdtt-cli.sh (утилита управления и бекапов)
├── setup.sh                            # Автоматический установщик
├── deploy.sh
├── API.md                              # Краткая справка по REST API
└── docs/                               # SERVER.md (архитектура) + API.md (детальный REST v2)
```

## Ключевые возможности v2.0

- ⚡ **Высокопроизводительный транспорт:** RAW UDP и DTLS туннели через VK TURN медиарелеи.
- 🎨 **Панель в стиле Remnawave:**
  - Живой поиск пользователей, фильтры статусов (*Все, Активны, Блокированы, Истекли*).
  - Быстрые действия в 1 клик: мгновенная активация/блокировка, сброс трафика, копирование подписки, QR-коды.
  - Календарь с пресетами срока (+30d, +90d, +180d, +1y) и тумблером бессрочного доступа.
- 🚀 **Полноценный RESTful API:**
  - Авторизация по статическому `X-API-Key` или JWT-токенам `Authorization: Bearer <token>`.
  - Никакой возни с сессионными cookies и CSRF-токенами для внешних ботов и интеграций.
  - Эндпоинты поиска, создания, блокировки, активации, сброса трафика и удаления пользователей.
  - Подробная документация: **[API.md](API.md)** и **[docs/API.md](docs/API.md)**.
- 🌐 **Кластерные ноды (Multi-Server):**
  - Подключение неограниченного числа внешних VPS прямо через веб-интерфейс или REST API.
  - Автоматическая генерация конфигураций и подписок со всеми серверами кластера.
- 💾 **Двойной движок БД (MongoDB + SQLite):**
  - Полная поддержка MongoDB через `MONGODB_URI` с автоматической миграцией из SQLite `panel.db`.
  - Двусторонняя синхронизация и прямой fallback без простоя.
- 📊 **Лимиты трафика и сроки действия:**
  - Мягкая политика: значения `0` или пустые означают **полный безлимит** и не мешают созданию пользователей.
  - Расширенный пул IP-адресов подсетей `10.66.66.X`–`10.66.75.X` на тысячи одновременных клиентов.
- 🛠️ **DevOps & CLI:**
  - Скрипт быстрой установки `setup.sh` (включая установку MongoDB и systemd).
  - Удобный CLI-алиас `wdtt` (`wdtt status`, `wdtt backup`, `wdtt restore`, `wdtt restart`, `wdtt log`).

---

## Происхождение

Серверная часть основана на проекте **[proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android)** (автор [amurcanov](https://github.com/amurcanov)) — WireGuard-туннель через DTLS-медиарелеи ВК.

WDTT расширяет upstream: мультипользователи, Remnawave-панель, Xray, ссылки `wdtt://`, Telegram-бот, MongoDB, REST API, многонодовый кластер, установщик. Подробности: **[CREDITS.md](CREDITS.md)**.

---

## Быстрая установка

### Вариант 1: Скрипт автоматической установки с MongoDB и CLI
```bash
sudo bash setup.sh
```
Скрипт автоматически:
1. Установит MongoDB (при необходимости) и зависимости.
2. Скомпилирует или скачает unified-бинарник `wdtt`.
3. Настроит systemd-сервисы и правила iptables.
4. Настроит глобальный алиас командной строки `wdtt`.

### Вариант 2: Онлайн-установщик
```bash
bash <(curl -Ls https://raw.githubusercontent.com/ildarmaga/wdtt-install/main/install.sh)
```

Панель: `http://IP:2860/wdtt/` (или `/`) — логин `admin` / `wdtt` (смените в настройках).

---

## Управление через CLI (`wdtt`)

После установки доступна удобная консольная утилита:
```bash
wdtt status           # Статус демона, MongoDB, портов и ресурсов
wdtt restart          # Перезапуск службы WDTT
wdtt log              # Просмотр логов в реальном времени (journalctl)
wdtt backup           # Создание резервной копии базы данных
wdtt restore <файл>   # Восстановление из архива
```

---

## Совместимые клиенты

| Клиент | Платформа | Формат ссылок |
|--------|-----------|---------------|
| **VK Turn Proxy** | iOS | `wdtt://` (colon), 1-й хеш или `VK_HASH` |
| **WDTT** | Android | `wdtt://` (colon), до 4 хешей через `,` или `VK_HASH` |
| **PWDTT** | Desktop Win/Linux | `wdtt://` (colon), до 4 хешей — `pwdtt-client-*` из [релизов WDTT](https://github.com/ildarmaga/wdtt/releases/latest); исходники: [ildarmaga/pwdtt-client](https://github.com/ildarmaga/pwdtt-client) |
| **WDTT** | Windows | `wdtt://` (colon), до 4 хешей через `,` или `VK_HASH` |
| **qWDTT** | Android (fork) | `qwdtt://`, `hashes=h1,h2,…` (bare, до 4) или `VK_HASH` |
| **WDTT** (qwdtt) | Windows | `qwdtt://`, `hashes=h1,h2,…` (bare, до 4) или `VK_HASH` |

---

## RESTful API панели

Для автоматизации, Telegram-ботов и интеграций доступен REST API без cookies:

```bash
# Получить список пользователей
curl -s -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users"

# Создать клиента на 30 дней и 100 ГБ трафика
curl -s -X POST -H "X-API-Key: ВАШ_КЛЮЧ" -H "Content-Type: application/json" \
  -d '{"name":"tg_user_123","total_gb":100,"expires_at":1762500000,"max_devices":2}' \
  "http://SERVER:2860/api/users"

# Заблокировать пользователя
curl -s -X POST -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users/tg_user_123/block"
```

Подробные спецификации и примеры на Python: **[API.md](API.md)** и **[docs/API.md](docs/API.md)**.

---

## Сборка

```bash
# Единый бинарник server+panel (основной, systemd → /usr/local/bin/wdtt)
./build.sh amd64 unified
sudo ./install-local.sh amd64

# Кросс-компиляция для Linux (без CGO)
CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o wdtt-linux-amd64 ./cmd/wdtt
```

Локальная разработка: `go.work` связывает корень (`pkg/`), `server/` и `panel/`.

---

## Лицензия

[GNU GPL v3](LICENSE) — как upstream [proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android).
