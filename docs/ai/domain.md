# Текущая доменная модель

Источник: `internal/services/{order,assignment,handover,warehouse_flow,incidents,assignment_expiry,dispatch_refresh}.go`,
`internal/models/`, миграции и OpenAPI. `CORE_DOMAIN_MODEL.md` — проект модели,
частично реализованный; его исходное описание проблем историческое.

## Order и Assignment

- Order: `draft`, `ready_for_dispatch`, `assigned`, `in_transit`, `arrived`,
  `completed`, `cancelled`; последние два терминальные.
- Assignment: `pending_acceptance`, `accepted`, `active`, `rejected`,
  `released`, `expired`, `completed`.
- Создание предложения, отказ и expiry не меняют `ready_for_dispatch` у заказа.
  Accept переводит заказ в `assigned`, Start — в `in_transit`, Arrive — в `arrived`,
  Complete — в `completed`. Assignment остаётся `active` при прибытии.
- Переназначение через `replacesAssignmentId` освобождает прежнее назначение
  и создаёт новое предложение в одной транзакции. Для `in_transit` применяется
  отдельный TripHandover с причиной/местом/состоянием груза и двумя подтверждениями.
  При приеме старое назначение освобождается, новое становится active; заказ
  сохраняет статус и исходный started_at. Старые участники остаются в истории.
- Старая ручка `PATCH /orders/{id}/status` возвращает 410.
- Клиент работает со своими заказами; менеджер читает входящие/исходящие своего склада;
  планирование и передача в пути принадлежат отправлению, приёмка назначения — получателю;
  администратор имеет глобальный доступ. Driver-команды проверяют принадлежность назначения.
- Создание менеджеров и водителей требует admin. Точные права отдельных команд
  смотреть в services/handlers, не выводить из общей таблицы ролей README.
- `GET /orders/{id}/references` возвращает только подписи участников/ресурсов этого
  заказа с теми же правами, что GetOrder. Словари users/drivers/vehicles/warehouses/assignments
  служат для отображения, UUID остаются идентификаторами команд и связей.

## Складские операции

Order хранит origin/destination warehouse IDs и снимки адресов. Receive после submit,
load перед start и unload после arrive фиксируются с количеством мест, состоянием,
actor/time и assignment ID. Для складского конца обязательна его часть процедуры.
После load нельзя обычным cancel/reassign менять фактически погруженный груз.
WarehouseStage — состояние рабочей очереди, основной Order.status не заменяется.
Снятие погрузки до выезда освобождает назначение; отменённый принятый груз
ожидает выдачи отправителю. Подробнее: [warehouses.md](warehouses.md).

OrderIncident хранит проблемы и однократные решения отдельно от lifecycle.
Open блокирует load/start/complete. Расхождение мест создаётся автоматически;
ответственный менеджер/admin фиксирует решение с объяснением.
Подробнее: [incidents.md](incidents.md).

## Стороны и приёмка

Counterparty — каталог пользователя с shared записями admin и архивацией.
Order.transportDetails хранит снимок sender/recipient/carrier, основания и числа мест;
после submit он неизменяем. Новый API-заказ имеет transportPolicyVersion=1,
старые записи — 0; заполнение реквизитов старого draft повышает версию.
DeliveryReceipt — отдельный неизменяемый факт приёмки после arrive. Для версии 1
complete требует эту запись и берёт имя из неё. Складская приёмка сверяется с unload;
адресная — с load либо заявкой, создавая проблему при расхождении.
Подробнее: [transport-accounting.md](transport-accounting.md).

## Бронирование и eligibility

- Открытые статусы: `pending_acceptance`, `accepted`, `active`.
- Один открытый Assignment на заказ: partial unique index.
- Водитель и машина не могут иметь открытые назначения с пересечением
  полуоткрытых интервалов `[planned_from, planned_to)`; authoritative guard — GiST exclusion.
- Блокировки команд: Order → Assignment → Driver → Vehicle;
  при замене несколько ресурсов одного типа сортируются по ID.
- Для назначения проверяются `available`, права, водительское удостоверение,
  регистрация машины, вместимость и смена, если у водителя есть смены.
- При отсутствии записей license в `driver_documents` используется `drivers.license_expiry`.
  При отсутствии смен расписание не ограничивает назначение.
- `drivers.vehicle_id` не является историческим источником машины рейса;
  конкретная машина хранится в Assignment. Занятость определяется назначениями.
- Dispatch даёт совет; Core снова проверяет ресурсы при бронировании.

## Отличия от целевого проекта

- Статусы автопарка ещё legacy (`available`, `on_route`/`in_transit`, `off_duty`/`maintenance`),
  отдельных eligibility/duty/operational моделей нет.
- Нет version-полей, command ID, loading/failed статусов и общего release endpoint.
  Удержание по открытым проблемам реализовано через OrderIncident.
- API черновика редактирует груз и окно, но не адреса; routing выполняется при CreateOrder.
- Вес/объём могут быть нулевыми; проект с обязательными положительными значениями
  не является текущим контрактом.
- Имя/роль получателя, основание, время, количество и состояние обязательны
  в DeliveryReceipt для новой процедуры. Legacy complete допускает отсутствие записи;
  подпись/фото и проверка полномочий отсутствуют. Handover требует двух подтверждений.
- История переходов не содержит всех полей аудита из целевого проекта.

Эти отличия — факты реализации, а не автоматически утверждённые задачи на разработку.
