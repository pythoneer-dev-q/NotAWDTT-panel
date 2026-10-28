# WDTT

[![Build and tests](https://github.com/pythoneer-dev-q/NotAWDTT-panel/actions/workflows/build-server.yml/badge.svg)](https://github.com/pythoneer-dev-q/NotAWDTT-panel/actions/workflows/build-server.yml)
[![Latest release](https://img.shields.io/github/v/release/pythoneer-dev-q/NotAWDTT-panel)](https://github.com/pythoneer-dev-q/NotAWDTT-panel/releases/latest)

[![Поддержать WDTT](https://devgamemaga.mooo.com:9443/b/soft.svg)](https://devgamemaga.mooo.com:9443/donate)

VPN на VPS через медиарелеи ВК (TURN / RAW UDP) с современной веб-панелью в стиле **Remnawave**, поддержкой **MongoDB**, многонодовых кластеров и полноценным **RESTful API**.

<!-- Скриншоты: сохраните изображения без персональных данных в docs/screenshots/
и добавьте сюда: ![Панель управления](docs/screenshots/dashboard.png) -->

Собранные бинарники для Linux amd64 и arm64 доступны в артефактах [GitHub Actions](https://github.com/pythoneer-dev-q/NotAWDTT-panel/actions/workflows/build-server.yml); для тегов версий они также публикуются в [релизах](https://github.com/pythoneer-dev-q/NotAWDTT-panel/releases).

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
  - Подсеть DTLS/WireGuard `10.66.0.0/16`: до 64 768 адресов устройств. Старые адреса `10.66.66.X`–`10.66.75.X` сохраняются.
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
Название проекта на странице подписки и в боковом меню задаётся переменной `WDTT_PROJECT_NAME` в `/etc/wdtt/.env`; после изменения перезапустите панель.

Оформление страницы подписки настраивается там же: `WDTT_SUB_PAGE_ACCENT`, `WDTT_SUB_PAGE_BACKGROUND`, `WDTT_SUB_PAGE_SURFACE`, `WDTT_SUB_PAGE_TEXT` (цвета `#RRGGBB`), `WDTT_SUB_PAGE_THEME` (`auto`, `light`, `dark`), `WDTT_SUB_PAGE_RADIUS` (0–32), `WDTT_SUB_PAGE_LOGO_URL` (HTTPS), `WDTT_SUB_PAGE_HEADING` и `WDTT_SUB_PAGE_INSTRUCTION` (обычный текст). Для редактирования используйте `sudo wdtt env`. Выбор темы пользователем в браузере имеет приоритет.
При интерактивной установке выберите «Только панель» или «Панель + нода». Для автоматизации: `sudo bash setup.sh --panel-only` либо `sudo bash setup.sh --panel-node`.
Скрипт автоматически:
1. Установит MongoDB (при необходимости) и зависимости.
2. Скомпилирует или скачает unified-бинарник `wdtt`.
3. Настроит systemd-сервисы и правила iptables.
4. Настроит глобальный алиас командной строки `wdtt`.

### Вариант 2: Онлайн-установщик
[Онлайн-установщик с выбором режима](https://github.com/pythoneer-dev-q/NotAWDTT-panel/blob/main/install.sh) запускается одной командой:
```bash
curl -fsSL https://raw.githubusercontent.com/pythoneer-dev-q/NotAWDTT-panel/main/install.sh | sudo bash
```
Ссылка начнёт работать после публикации `install.sh` в доступной ветке `main` этого репозитория.
Прежний [расширенный установщик](https://github.com/ildarmaga/wdtt-install/blob/main/install.sh) доступен отдельно.

### Подключение дополнительного VPS

1. Откройте панель по HTTPS, создайте сервер в разделе «Ноды» и скопируйте команду и секрет отдельно. Команда загружает [публичный установщик ноды с GitHub](https://raw.githubusercontent.com/pythoneer-dev-q/NotAWDTT-panel/main/node-install.sh); он запрашивает секрет в SSH-терминале и по HTTPS получает [персональный установщик](panel/node-install.sh.tmpl) с панели. Секрет не включается в ссылку или историю команд.
2. Запустите команду на Debian/Ubuntu VPS с правами root и вставьте секрет в запрос. Установщик проверит HTTPS-сертификат панели, доступность API-ключа и TUN, загрузит бинарник, откроет UDP-порты и создаст `wdtt-node.service`. Для самоподписанного сертификата панели используется привязка к его публичному ключу.
3. Панель покажет стадии «Создана → Установщик → Онлайн». После первого ответа ноды будет виден SHA-256 отпечаток её DTLS-сертификата. Если нода остаётся без связи, выполните на VPS `sudo wdtt-node logs` и откройте указанные UDP-порты в firewall облачного провайдера.

VPS должен достигать API панели по HTTPS не только во время установки, но и при работе ноды. Если прямой маршрут заблокирован, укажите в карточке установки доступный с VPS HTTPS-адрес панели или настройте на VPS `HTTPS_PROXY` до запуска; установщик сохранит его для службы. Проверьте с VPS `curl -I https://адрес-панели/wdtt/panel/api/node/sync`: ответ HTTP 403 подтверждает, что адрес доступен (секрет в этой проверке не передаётся). GitHub также должен быть доступен для первоначальной загрузки; `wdtt-node reinstall` при недоступности GitHub использует панель как резервный источник.

Для прокси с паролем войдите на VPS под root и задайте его без записи в историю: `read -rsp 'HTTPS_PROXY: ' HTTPS_PROXY; echo; export HTTPS_PROXY`. Затем запустите команду из панели. Если VPS не может достичь панели ни напрямую, ни через прокси, установка ноды не обеспечит её дальнейшую синхронизацию.

На VPS доступны `sudo wdtt-node status`, `logs`, `restart`, `reinstall`, `uninstall`. API-ключ хранится в `/etc/wdtt-node/node.conf` с правами `600`. При разной архитектуре панели и VPS заранее положите соответствующий `wdtt-linux-amd64` или `wdtt-linux-arm64` в `/etc/wdtt/bin/` панели.

### Telegram-бот

Бот работает как клиент REST API v2 и поэтому видит пользователей, трафик и все ноды кластера даже в режиме «только панель». Настройте `/etc/wdtt/.env` (при локальном запуске — `.env`):

```dotenv
WDTT_BOT_ENABLED=true
WDTT_BOT_TOKEN=123456:telegram-token
WDTT_BOT_ADMIN_IDS=123456789,987654321
WDTT_BOT_PANEL_URL=http://127.0.0.1:2860/api
# Обычно оставьте пустым: встроенный бот возьмёт API-ключ панели автоматически.
WDTT_BOT_API_KEY=
```

После изменения перезапустите панель: `sudo systemctl restart wdtt-panel` (либо `wdtt`, если используется unified-служба). Пользователь Telegram с ID `123` связан с учёткой по комментарию `tg_123`; пароль подключения генерируется случайно. Администратор выдаёт её командой `/grant 123 [дни] [ГБ] [устройства]`; нулевые срок и трафик означают безлимит, а ноль устройств панель заменяет значением по умолчанию. В карточке отображаются общий трафик, upload/download, устройства, online/last seen, до пяти кнопок копирования нод и подписка со всеми остальными нодами.

Основные команды администратора: `/admin`, `/users`, `/find`, `/grant`, `/nodes`, `/status`, `/restart`. Все настройки и значения по умолчанию перечислены в [.env.example](.env.example); токен и API-ключ не добавляйте в Git.
После обновления панели выполните на внешней VPS `sudo wdtt-node reinstall`: нода получит новый бинарник и MTU-правила. При первом запуске нода получает пароли при синхронизации; после этого хранит их в локальной `/etc/wdtt-node/panel.db` и может перезапуститься во время временной недоступности панели. Ошибки соединения с панелью смотрите через `sudo wdtt-node logs`.
Для клиентов DTLS/GETCONF на ноде теперь по умолчанию запускается WireGuard (`wdtt0`). Если WireGuard был вручную выключен у локального подключения, включите «WireGuard внутри DTLS» в настройках WDTT; там же можно увеличить лимит пользователей до 64 768. Новые подключения получают адреса из `10.66.0.0/16`, а уже выданные адреса сохраняются.
У ссылки VK Turn Proxy можно задать ремарку сервера в карточке пользователя: она добавится после `#`.

Панель: `http://IP:2860/wdtt/` (или `/`) — логин `admin` / `wdtt` (смените в настройках).

---

## Управление через CLI (`wdtt`)

После установки доступна удобная консольная утилита:
```bash
wdtt status           # Статус демона, MongoDB, портов и ресурсов
wdtt restart          # Перезапуск службы WDTT
wdtt reinstall        # Переустановка с сохранением конфигурации и MongoDB
wdtt uninstall        # Удаление службы и бинарников с сохранением данных
wdtt log              # Просмотр логов в реальном времени (journalctl)
wdtt backup           # Создание резервной копии базы данных
wdtt restore <файл>   # Восстановление из архива
sudo bash update.sh --log-level warn   # Обновление с уменьшенным уровнем логов (debug/info/warn/error)
sudo bash update.sh --check            # Сверить исходники и установленный бинарник с GitHub main
sudo wdtt update                       # Обновить из установленного репозитория
```

В карточке пользователя доступны лимит трафика в ГБ, срок доступа, число устройств и шейпинг загрузки и отдачи в Мбит/с. `0` для трафика или скорости снимает соответствующее ограничение. Скорость меняется для активных подключений; настройки передаются внешним нодам при синхронизации каждые 10 секунд. При нескольких нодах квота трафика может быть превышена на объём, переданный между синхронизациями. Для применения новой версии на ноде после обновления панели запустите на VPS `sudo wdtt-node reinstall`; её версию и необходимость обновления видно на странице «Ноды».

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
