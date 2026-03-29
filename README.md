# Logiflow

Бэкенд для логистической платформы. Управление заказами на перевозку, водителями, транспортом и складами. При создании заказа строится реальный маршрут через OSRM, считается стоимость перевозки, трекинг водителя в реальном времени через WebSocket.

## Стек

- **Go 1.25** — Chi v5, oapi-codegen, pgx/v5, go-redis
- **PostgreSQL** — миграции через Goose
- **Redis** — хранение JWT access/refresh токенов
- **Nominatim** — геокодинг адресов (OpenStreetMap, без ключа)
- **OSRM** — построение маршрутов и расчёт дистанции
- **Prometheus + Grafana** — мониторинг
- **Podman** — контейнеризация

## Запуск

```bash
# поднять все сервисы
make up

# пересобрать и поднять
make build-up

# остановить
make down

# для деплоя (pull + build + up)
make deploy
```

Перед запуском создать сеть:
```bash
podman network create LogiflowNetwork
```

Скопировать `.env` и заполнить секреты:
```bash
cp .env.example .env
```

Обязательные переменные:
```
LOGIFLOW_DATABASE_USER=
LOGIFLOW_DATABASE_PASSWORD=
LOGIFLOW_DATABASE_NAME=
LOGIFLOW_REDIS_PASSWORD=
LOGIFLOW_JWT_KEY=
```

## Конфигурация

Конфиг читается из `configs/config.toml`, переменные окружения с префиксом `LOGIFLOW_` перекрывают файл.

```toml
[pricing]
baseFee = 5000.0   # базовая ставка, руб
perKm   = 100.0    # руб/км
perKg   = 15.0     # руб/кг
perM3   = 600.0    # руб/м³
```

Цена заказа считается по формуле:
```
total = baseFee + distance_km * perKm + weight_kg * perKg + volume_m3 * perM3
```

## Роли

| Роль | Возможности |
|---|---|
| `client` | Создаёт заказы, следит за своими заявками |
| `driver` | Меняет свой статус, видит назначенные маршруты |
| `manager` | Назначает водителей на заказы |
| `admin` | Создаёт профили водителей и менеджеров, видит всё |

Клиенты регистрируются сами через `POST /auth/register`. Водителей и менеджеров создаёт только администратор.

## Авторизация

JWT (HS256) + refresh токены. Access токен живёт 24 часа, refresh — 7 дней в HTTP-only cookie. Оба хранятся в Redis — при логауте удаляются.

```
Authorization: <access_token>
```

## Флоу заказа

```
Клиент создаёт заказ (адреса → Nominatim → координаты → OSRM → маршрут)
  ↓
Менеджер назначает водителя (pending → assigned)
  ↓
Водитель начинает поездку (assigned → in_transit)
  ↓
Трекинг по WebSocket — current_index двигается по массиву координат
  ↓
Водитель завершает (in_transit → delivered)
```

## Структура БД

```
users
  ├── drivers (user_id) → vehicles
  └── managers (user_id) → warehouses

orders (created_by_id → users, driver_id → drivers, manager_id → managers)
  └── routes (order_id) — JSONB координаты маршрута, current_index

notifications (user_id → users)
```

Сервер поднимается на `localhost:3001`, Grafana на `localhost:3000`, Prometheus на `localhost:9090`.
