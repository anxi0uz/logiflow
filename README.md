# Logiflow

Бэкенд для логистической платформы. Управление заказами на перевозку, назначениями водителей и транспорта, складами и маршрутами. При создании заказа строится маршрут через OSRM и рассчитывается стоимость перевозки. Текущий WebSocket-трекинг является демонстрационной симуляцией движения по маршруту.

- **Backend:** [github.com/anxi0uz/logiflow](https://github.com/anxi0uz/logiflow)
- **Frontend:** [github.com/siers22/logiflow-frontend](https://github.com/siers22/logiflow-frontend)
- **Демо:** [logiflowadvanced.online](https://logiflowadvanced.online)

> Демо развёрнуто в Kubernetes (k3s).

## Стек

**Backend:**
- **Go 1.25** — Chi v5, oapi-codegen, pgx/v5, go-redis, errgroup
- **PostgreSQL** — миграции через Goose
- **Redis** — хранение JWT access/refresh токенов
- **Nominatim** — геокодинг адресов (OpenStreetMap, без ключа)
- **OSRM** — построение маршрутов и расчёт дистанции
- **Prometheus + Grafana** — мониторинг HTTP метрик
- **Podman** — контейнеризация

**Frontend:**
- Репозиторий: [github.com/siers22/logiflow-frontend](https://github.com/siers22/logiflow-frontend)

## Запуск

### Зависимости

**Linux:**
- [Podman](https://podman.io/docs/installation) + [podman-compose](https://github.com/containers/podman-compose)

```bash
# Arch
sudo pacman -S podman
pip install podman-compose
# Или
yay -S podman-compose

# Ubuntu/Debian
sudo apt install podman
pip install podman-compose
```

**Windows:**
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) — включает docker-compose

---

### 1. Клонировать репозиторий

```bash
git clone https://github.com/anxi0uz/logiflow.git
cd logiflow
```

---

### 2. Заполнить `.env`

В корне проекта уже есть `.env` — просто заполни значения:

```env
LOGIFLOW_DATABASE_USER=logiflow
LOGIFLOW_DATABASE_PASSWORD=yourpassword
LOGIFLOW_DATABASE_NAME=logiflow
LOGIFLOW_REDIS_PASSWORD=yourredispassword
LOGIFLOW_JWT_KEY=your-secret-jwt-key-min-32-chars
```

> `LOGIFLOW_JWT_KEY` — любая случайная строка, минимум 32 символа.

---

### 3. Создать сеть (только Linux / Podman)

```bash
podman network create LogiflowNetwork
```

На Windows Docker создаёт сеть автоматически — этот шаг пропускай.

---

### 4. Запустить

**Linux (Podman):**
```bash
make up        # запуск
make build-up  # пересборка образа + запуск
make down      # остановка
make deploy    # pull + пересборка + запуск (для деплоя)
```

**Windows (Docker):**
```bat
docker compose up -d
docker compose build && docker compose up -d
docker compose down
```

Миграции БД применяются **автоматически** при старте приложения — делать ничего не нужно.

---

### 5. Проверить

Проверить liveness приложения:

```bash
curl http://localhost:3001/health/live
```

`/health/ready` дополнительно проверяет соединения с PostgreSQL и Redis, `/metrics` отдаёт метрики Prometheus.

---

## Сервисы

| Сервис | Адрес |
|---|---|
| API | `http://localhost:3001` |
| Grafana | `http://localhost:3000` |
| Prometheus | `http://localhost:9090` |
| Метрики | `http://localhost:3001/metrics` |
| Liveness | `http://localhost:3001/health/live` |
| Readiness | `http://localhost:3001/health/ready` |

## Конфигурация

Конфиг читается из `configs/config.toml`, переменные окружения с префиксом `LOGIFLOW_` перекрывают файл.

```toml
[server]
host         = "0.0.0.0"
port         = 3001
readTimeout  = "10s"
writeTimeout = "30s"
idleTimeout  = "60s"

[redis]
refreshTokenTTL = "168h"
accessTokenTTL  = "24h"

[pricing]
baseFee = 500.0   # базовая ставка, руб
perKm   = 25.0    # руб/км
perKg   = 3.0     # руб/кг
perM3   = 150.0   # руб/м³
```

Цена заказа считается по формуле:
```
total = baseFee + distance_km * perKm + weight_kg * perKg + volume_m3 * perM3
```

## Роли

| Роль | Возможности |
|---|---|
| `client` | Создаёт, отправляет на диспетчеризацию и отменяет свои заказы, следит за статусом |
| `driver` | Принимает или отклоняет назначение, начинает перевозку, подтверждает прибытие и доставку |
| `manager` | Назначает водителя и транспорт, переназначает ресурсы, отменяет заказы, смотрит отчёты |
| `admin` | Создаёт профили водителей, видит всё |

Клиенты регистрируются через `POST /auth/register`. Роль назначается вручную в БД. Водителей создаёт `admin`, менеджеров — авторизованный пользователь.

## Авторизация

JWT (HS256) + refresh токены. Access токен живёт 24 часа, refresh — 7 дней в HTTP-only cookie. Оба хранятся в Redis — при логауте удаляются.

```
Authorization: Bearer <access_token>
```

## API

### Auth
| Метод | Путь | Описание |
|---|---|---|
| POST | `/auth/register` | Регистрация клиента |
| POST | `/auth/login` | Вход, получение токенов |
| POST | `/auth/logout` | Выход, инвалидация токенов |
| POST | `/auth/refresh` | Обновление access токена |

### Пользователь
| Метод | Путь | Описание |
|---|---|---|
| GET | `/me` | Профиль текущего пользователя |
| PATCH | `/me` | Обновить имя, аватар, пароль |
| DELETE | `/me` | Удалить аккаунт |
| GET | `/me/trips` | История поездок (driver) |

### Заказы
| Метод | Путь | Описание |
|---|---|---|
| GET | `/api/v1/orders` | Список заказов (по роли) |
| POST | `/api/v1/orders` | Создать заказ |
| GET | `/api/v1/orders/{id}` | Получить заказ |
| POST | `/api/v1/orders/{id}/submit` | Подготовить заказ к диспетчеризации |
| POST | `/api/v1/orders/{id}/cancel` | Отменить заказ |
| PATCH | `/orders/{id}/status` | Устаревшая ручка, всегда возвращает `410 Gone` |
| GET | `/orders/{id}/route` | Маршрут заказа |
| GET | `/orders/{id}/route/ws` | WebSocket трекинг |

### Назначения
| Метод | Путь | Описание |
|---|---|---|
| POST | `/api/v1/orders/{id}/assignments` | Предложить водителя и транспорт |
| POST | `/api/v1/assignments/{id}/accept` | Принять назначение |
| POST | `/api/v1/assignments/{id}/reject` | Отклонить назначение |
| POST | `/api/v1/assignments/{id}/start` | Начать перевозку |
| POST | `/api/v1/assignments/{id}/arrive` | Подтвердить прибытие |
| POST | `/api/v1/assignments/{id}/complete` | Подтвердить доставку |

### Водители
| Метод | Путь | Описание |
|---|---|---|
| GET | `/drivers` | Список водителей |
| POST | `/drivers` | Создать водителя (admin) |
| GET | `/drivers/{slug}` | Получить водителя |
| PUT | `/drivers/{slug}` | Обновить водителя |
| DELETE | `/drivers/{slug}` | Удалить водителя |
| PATCH | `/drivers/me/status` | Обновить свой статус (driver) |

### Менеджеры
| Метод | Путь | Описание |
|---|---|---|
| GET | `/managers` | Список менеджеров |
| POST | `/managers` | Создать менеджера |
| GET | `/managers/{slug}` | Получить менеджера |
| DELETE | `/managers/{slug}` | Удалить менеджера |

### Склады
| Метод | Путь | Описание |
|---|---|---|
| GET | `/warehouses` | Список складов |
| POST | `/warehouses` | Создать склад |
| GET | `/warehouses/{slug}` | Получить склад |
| PUT | `/warehouses/{slug}` | Обновить склад |
| DELETE | `/warehouses/{slug}` | Удалить склад |

### Транспорт
| Метод | Путь | Описание |
|---|---|---|
| GET | `/vehicles` | Список ТС |
| POST | `/vehicles` | Добавить ТС |
| GET | `/vehicles/{slug}` | Получить ТС |
| PUT | `/vehicles/{slug}` | Обновить ТС |
| DELETE | `/vehicles/{slug}` | Удалить ТС |

### Отчёты
| Метод | Путь | Описание |
|---|---|---|
| GET | `/reports/orders` | Отчёт по заказам (manager) |
| GET | `/reports/dashboard` | Дашборд (manager) |

### Уведомления
| Метод | Путь | Описание |
|---|---|---|
| GET | `/notifications` | Список уведомлений |
| PATCH | `/notifications/{id}/read` | Отметить прочитанным |

## Флоу заказа

```
Клиент создаёт черновик заказа (draft)
  ↓
Клиент отправляет заказ на диспетчеризацию (ready_for_dispatch)
  ↓
Менеджер предлагает Driver + Vehicle (Assignment pending_acceptance)
  ↓
Водитель принимает назначение (Order assigned, Assignment accepted)
  ↓
Водитель начинает перевозку (in_transit / active)
  ↓
Водитель подтверждает прибытие (arrived)
  ↓
Водитель или менеджер подтверждает доставку (completed)
```

`completed` и `cancelled` — терминальные состояния. Занятость водителя и транспорта определяется открытыми Assignment с пересекающимся временным окном, а не ручным переключением `available`.

## Архитектура

```
cmd/
└── main.go               — точка входа, инициализация зависимостей

internal/
├── api/                  — OpenAPI спека + сгенерированный oapi-codegen код
├── config/               — загрузка конфига (TOML + ENV)
├── database/             — подключение к PostgreSQL и Redis, запуск миграций
├── handler/              — HTTP хендлеры (Chi), WebSocket хаб, Prometheus middleware
├── models/               — структуры БД
└── services/             — бизнес-логика заказов, назначений, eligibility и отчётов

pkg/
├── storage.go            — generic CRUD поверх pgx (GetOne, GetAll, Create, Update)
└── geocode/              — клиенты Nominatim (геокодинг) и OSRM (маршруты)

migrations/               — SQL миграции Goose, применяются автоматически при старте
configs/                  — config.toml, prometheus.yml, datasources Grafana
```

**Слои и зависимости:**

```
HTTP-запрос
  → Chi router
  → Auth middleware (JWT → Redis)
  → Handler
  → Service (бизнес-логика, внешние API)
  → pkg/storage (generic SQL)
  → PostgreSQL
```

**Внешние сервисы:**
- **Nominatim** — геокодинг адресов при создании заказа и склада
- **OSRM** — построение маршрута и расчёт дистанции/времени
- **Redis** — хранение access/refresh токенов, инвалидация при логауте
- **Prometheus** — сбор метрик с `/metrics`
- **Grafana** — дашборд метрик

![Architecture scheme](docs/infra.jpg)

## Структура БД

![DB Schema](docs/db.jpg)

## Мониторинг

Prometheus собирает метрики с `/metrics`. Grafana доступна на `localhost:3000` (admin/admin).

Доступные метрики:
- `logiflow_http_requests_total` — кол-во запросов по методу, пути, статусу
- `logiflow_http_duration_seconds` — latency запросов

Конфиг Prometheus: `configs/prometheus.yml`
Datasource Grafana: `configs/datasources/`

## Тесты

Обычный прогон включает domain-, handler- и HTTP smoke-тесты. Интеграционные тесты автоматически пропускаются без переменных окружения:

```bash
go test ./...
```

Проверка Core на настоящей PostgreSQL, включая полный lifecycle и конкурентное назначение одного ресурса:

```bash
LOGIFLOW_TEST_DATABASE_URL='postgres://user:password@localhost:5432/logiflow_test?sslmode=disable' \
go test ./internal/services -run TestCoreLifecycleAndConcurrentReservation -count=1 -v
```

Проверка refresh-token rotation требует PostgreSQL и отдельную тестовую Redis:

```bash
LOGIFLOW_TEST_DATABASE_URL='postgres://user:password@localhost:5432/logiflow_test?sslmode=disable' \
LOGIFLOW_TEST_REDIS_ADDR='localhost:6379' \
go test ./internal/handler -run TestRefreshTokenIdentifiesUserAndRotates -count=1 -v
```
