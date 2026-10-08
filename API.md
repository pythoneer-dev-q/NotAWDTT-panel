# WDTT RESTful API v2 (Remnawave-style)

Полная документация по RESTful API для панели WDTT. Подробное руководство доступно в [docs/API.md](docs/API.md).

---

## Быстрый старт

### Аутентификация
Вместо куки и CSRF используйте заголовок **`X-API-Key`** или **`Authorization: Bearer <token>`**:
```bash
curl -H "X-API-Key: ВАШ_КЛЮЧ" "https://SERVER:2860/api/users"
```

> API-ключ генерируется автоматически при первом запуске и отображается в веб-панели в окне **«REST API»**.

---

## Сводка эндпоинтов

### Аутентификация
- `POST /api/auth/token` — получение JWT токена (`{"username": "admin", "password": "..."}`)
- `GET /api/auth/me` — проверка токена

### Пользователи
- `GET /api/users?q={query}&status={active|blocked|expired|exceeded}&limit=50&offset=0` — поиск и фильтрация
- `POST /api/users` — создание пользователя (`total_gb`, `expires_at`, `max_devices`). Значения `0` = безлимит!
- `GET /api/users/{id}` — полная карточка, ссылки подключения со всех нод и подписка
- `PUT` / `PATCH /api/users/{id}` — обновление данных пользователя
- `DELETE /api/users/{id}` — удаление пользователя
- `POST /api/users/{id}/activate` — активация / разблокировка
- `POST /api/users/{id}/block` — блокировка
- `POST /api/users/{id}/reset-traffic` — сброс накопленного трафика в 0

### Кластерные ноды
- `GET /api/nodes` — список подключённых внешних VPS
- `POST /api/nodes` — добавление новой ноды
- `DELETE /api/nodes/{id}` — удаление ноды
- `POST /api/nodes/{id}/ping` — проверка доступности ноды

### Система
- `GET /api/status` — статус сервера, память, службы, трафик
- `POST /api/system/restart` — перезапуск VPN демона

---

## Примеры cURL

```bash
# 1. Список активных пользователей
curl -s -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users?status=active"

# 2. Создание пользователя на 30 дней и 100 ГБ
curl -s -X POST -H "X-API-Key: ВАШ_КЛЮЧ" -H "Content-Type: application/json" \
  -d '{"name":"tg_user_123","total_gb":100,"expires_at":1762500000,"max_devices":2}' \
  "http://SERVER:2860/api/users"

# 3. Блокировка
curl -s -X POST -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users/tg_user_123/block"

# 4. Активация
curl -s -X POST -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users/tg_user_123/activate"

# 5. Сброс трафика
curl -s -X POST -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users/tg_user_123/reset-traffic"

# 6. Удаление
curl -s -X DELETE -H "X-API-Key: ВАШ_КЛЮЧ" "http://SERVER:2860/api/users/tg_user_123"
```

Подробные примеры на Python и описание структур данных см. в [docs/API.md](docs/API.md).
