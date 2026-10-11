# Архитектура

## Core

- `cmd/main.go`: конфиг, подключения, Goose, HTTP и фоновые обработчики.
- `internal/handler/`: Chi HTTP, auth, документы/Dispatch через gRPC, WebSocket.
- `internal/services/`: команды заказа/назначения, проверки ресурсов, отчёты.
- `internal/models/`, `migrations/`: текущая модель PostgreSQL.
- `internal/events/`: relay outbox и приём результатов документов/Dispatch.
- `pkg/storage.go`: generic SQL/pgx; `pkg/routing/`: маршрут; `pkg/geocode/`: геокодинг.
- `internal/api/api.swagger.yaml`: HTTP-контракт; `internal/api/gen.go` генерируется.
- Redis хранит access/refresh токены. Readiness проверяет PostgreSQL и Redis.

Order, Assignment и Fleet разделены логически, но изменения остаются внутри
одного Core и его транзакционной границы PostgreSQL. Складские операции
`warehouse_operations` находятся в той же границе: проверки start/complete
и подтверждения менеджеров сериализуются блокировкой заказа. В той же границе
находятся `order_incidents`: автоматическое расхождение атомарно с операцией склада,
решение проблемы и удержание load/start/complete используют ту же блокировку Order.

## Dispatch

`services/dispatch/`, Python; приватный gRPC 50052. Своей БД нет.
Роль `dispatch_reader` читает только семь таблиц заказа/автопарка из Core;
репозиторий использует read-only repeatable-read транзакцию.
SQL фильтрует документы, смены, вместимость и пересечение бронирований;
загружается до 30 водителей и 30 машин. Ранжирование учитывает рейтинг
водителя и соответствие вместимости. География и оптимизация нескольких заказов отсутствуют.
Результат хранит Core в `dispatch_recommendations`; рекомендация не бронирует ресурс.

## Documents

`services/documents/`, Python; своя PostgreSQL и Alembic, приватный gRPC 50051.
Хранит шесть видов внутренних PDF в BYTEA и собственный outbox.
Публичный доступ только через авторизованный Core: `/api/v1/documents`
и `/api/v1/documents/{id}/download`; доступ определяется владельцем и
`document_recipients`, документ без доступа возвращает 404.
Генерация требует кириллический шрифт DejaVu Sans, включённый в Dockerfile.

## События

| Subject | Производитель → потребитель | Назначение |
| --- | --- | --- |
| `dispatch.requested.v1` | Core → Dispatch | ID заказа, submitted_at, requested_at; чтение после commit |
| `dispatch.recommended.v1` | Dispatch → Core | Ранжированные пары; защита от устаревшего результата |
| `delivery.completed.v1` | Core → Documents | Неизменяемый снимок доставки |
| `document.requested.v1` | Core → Documents | Тип, reference_id, поля снимка, получатели остальных внутренних документов |
| `document.ready.v1` | Documents → Core | Ссылка на документ для уведомления |
| `invalid.events.v1` | обработчики → diagnostic stream | Невалидные события и исходные bytes |

JetStream: `LOGIFLOW_EVENTS` и `LOGIFLOW_INVALID_EVENTS`.
Изменение домена и Core outbox фиксируются вместе. Документ уникален по
`reference_id + type`: несколько передач одного заказа получают разные PDF.
Повторная доставка допустима;
inbox/уникальность предотвращают повторный бизнес-эффект. Временные ошибки оставляют
сообщение для retry; невалидное подтверждается после сохранения диагностики.

## Фоновые задачи Core

- Assignment expiry: при старте и раз в 30 секунд, до 100 предложений за проход;
  NOWAIT и курсор не дают занятым строкам блокировать следующие страницы.
- Dispatch refresh: при старте и раз в секунду; одна страница до 100 заказов
  на запись `dispatch_release_queue`, курсор хранится в БД.
- Освобождение брони и запись в release queue атомарны и не требуют доступного NATS.
- Очередь разворачивается в outbox для ожидающих заказов с пересекающимся окном;
  неотправленные запросы одного заказа объединяются.

Compose — `podman-compose.yml`; отдельный CI/E2E стенд — `.github/compose.e2e.yml`.
Kubernetes manifests в этом репозитории отсутствуют; README не доказывает состояние деплоя.
