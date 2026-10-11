# Разработка и проверка

## Среда

Go version и зависимости — `go.mod`; Python поддерживается от 3.13,
CI и образы используют 3.13. В каждом Python-сервисе свой `uv.lock`.
Конфиг Core — `configs/config.toml` с override `LOGIFLOW_*`.
Секреты и содержимое `.env` в AI-документацию не копировать.

Основной стек: `podman-compose.yml`, внешняя сеть `LogiflowNetwork`,
команды `make up`, `make build-up`, `make down`.
При старте Core применяет Goose; Documents применяет Alembic.
`make deploy` останавливает стек и делает git pull, поэтому не использовать его для проверки.

## Проверки

```bash
go test ./...
```

В `services/dispatch` и `services/documents`:

```bash
uv sync --locked --group dev
uv run --no-sync pytest -q
uv run --no-sync ruff check .
uv run --no-sync ruff format --check .
```

PDF-тесты используют `FONT_PATH` либо DejaVuSans.ttf в
`/usr/share/fonts/truetype/dejavu/` или `/usr/share/fonts/TTF/`.
В контейнере нужные шрифты уже установлены; CI устанавливает fonts-dejavu-core.

DB integration tests требуют `LOGIFLOW_TEST_DATABASE_URL`; auth также требует
`LOGIFLOW_TEST_REDIS_ADDR` с Redis без пароля. Использовать только отдельную тестовую
БД: часть тестов делает TRUNCATE. Пакеты запускать последовательно (`go test -p 1`)
или выдавать каждому отдельную БД, чтобы избежать конфликтов миграций/данных.

HTTP E2E: `.github/compose.e2e.yml`, `tests/e2e/test_core_http_flow.py`.
Требуются `LOGIFLOW_E2E_BASE_URL` и `LOGIFLOW_E2E_DATABASE_URL` с БД строго
`logiflow_test`; дополнительные переменные:
`LOGIFLOW_E2E_DISPATCH_DATABASE_URL` для прав reader и `LOGIFLOW_E2E_DOCUMENTS=1` для PDF.
Стенд использует OSRM stub, поэтому не зависит от публичного routing.

OpenAPI regeneration из корня:

```bash
go tool oapi-codegen --config configs/oapi-cfg.yaml internal/api/api.swagger.yaml
```

## Состояние проверки 2026-10-11

- Go: `go test -p 1 -count=1 -json ./...` с PostgreSQL и отдельной Redis —
  142 успешных теста и подтеста, ни одного пропущенного или упавшего теста;
  включены складские подтверждения, права менеджеров, гонка приёмки, прямой маршрут
  и все три handover-теста. Проверены снятие погрузки/замена машины,
  недостача вплоть до нулевой приёмки, выдача после отмены, повреждение,
  отказ получателя и конкурентное решение проблемы.
  `go vet ./...` также прошёл; golangci-lint локально не запускался.
- Dispatch: все 5 pytest прошли на хосте и в образе с Python 3.13.
- Documents: все 18 pytest прошли с DejaVu Sans; проверяются шесть типов,
  кириллица, длинные поля, повторное событие, получатели и невалидные события.
- Ruff lint/format обоих Python-сервисов прошли с locked зависимостями на хосте.
- Полный HTTP E2E прошёл с `LOGIFLOW_E2E_DOCUMENTS=1` и проверкой прав reader:
  рекомендации, отказ/пересчёт, две передачи в пути, история прежнего водителя,
  приёмка/погрузка/разгрузка, доставка/PDF, права участников, отмена и конкурентное бронирование.
  Дополнительно: отказ получателя с правами и обязательной разгрузкой перед решением,
  автоматическая недостача/блокировка complete, снятие погрузки и новая бронь,
  возврат отправителю после отмены. Расширенный сценарий прошёл за 16,01 с.
- Alembic проверен на старой схеме: сохранение существующего PDF и backfill reference_id;
  несколько протоколов передачи одного заказа получают разные reference_id.
- Шесть демонстрационных PDF в `output/pdf/` отрендерены и визуально проверены.
- Основной пользовательский API ответил `ready`. Тесты выполнялись в отдельном
  Podman compose-проекте с отдельными PostgreSQL/Redis/NATS и локальным OSRM stub.
  Новые Core/Documents образы собраны для изолированного стенда. При нестабильном
  rootless port forwarding использован доступ из `podman unshare --rootless-netns`.
  При сборке Core компилятор получил signal: killed; успешная пересборка выполнена
  через `podman build --env GOFLAGS=-p=1`, затем HTTP E2E прошёл.
  Один полный Go-прогон был прерван SIGKILL дочернего процесса тестовой PostgreSQL;
  причина внешнего сигнала не установлена, повторный полный прогон прошёл.
  Основной пользовательский стек оставлен работающим на прежних образах;
  обновление через `make build-up` применит новые миграции при старте.

CI содержит Go lint/tests, pytest/Ruff обоих сервисов и HTTP E2E.
Go job поднимает PostgreSQL/Redis и запускает пакеты последовательно с test env,
чтобы integration tests не пропускались. Удалённый CI в этой сессии не запускался.
