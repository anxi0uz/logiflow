# Logiflow Core Domain Model

## Order lifecycle, Assignment и Fleet eligibility/availability

Статус документа: архитектурный проект, без реализации.

Главное решение: в MVP Order, Assignment и Fleet остаются внутри одного Core и одной транзакционной границы PostgreSQL, но становятся отдельными доменными моделями. Dispatch только ранжирует кандидатов. Сначала формируется корректная бизнес-модель, после этого границы можно физически переносить в сервисы.

## 1. Проблемы текущей реализации

Сейчас Order.Status — строка в internal/models/order.go, а orders.status — VARCHAR без CHECK в migrations/20260303130520_create_order.sql.

OrderService.UpdateOrderStatus в internal/services/order.go:

- не проверяет пару current state → requested state;
- допускает любой статус из enum OpenAPI;
- совмещает lifecycle, assignment и notifications;
- принимает driverId, но не фиксирует vehicleId;
- не проверяет capacity, документы и занятость;
- не защищает от конкурентного назначения одного ресурса.

Текущий drivers.vehicle_id не может исторически доказать, какая машина выполняла рейс: связь может измениться после завершения заказа.

## 2. Минимальная Order state machine

Ожидание ответа водителя и отказ — состояния Assignment, а не Order. Пока водитель не принял предложение, Order остаётся ready_for_dispatch.

Не нужны одновременно delivered и completed:

- arrived означает физическое прибытие;
- completed означает подтверждение передачи груза и терминальное завершение.

### MVP transitions

    draft
      ├─ SubmitOrder → ready_for_dispatch
      └─ CancelOrder → cancelled

    ready_for_dispatch
      ├─ AssignmentAccepted fact → assigned
      └─ CancelOrder → cancelled

    assigned
      ├─ StartDelivery → in_transit
      ├─ ReleaseAcceptedAssignment → ready_for_dispatch
      └─ CancelOrder → cancelled

    in_transit
      └─ ConfirmArrival → arrived

    arrived
      └─ ConfirmDelivery → completed

    completed
      └─ terminal

    cancelled
      └─ terminal

Создание, отказ и истечение предложения не меняют статус Order:

    ready_for_dispatch
      ├─ ProposeAssignment → Order без изменения
      ├─ RejectAssignment → Order без изменения
      └─ AssignmentExpired → Order без изменения

Это lifecycle другого агрегата, а не self-transition Order.

### Зачем нужен draft

Draft позволяет сохранить неполный заказ, редактировать cargo/endpoints/window и не запускать routing/pricing при каждом сохранении. SubmitOrder проверяет полноту и переводит заказ в ready_for_dispatch.

UI может создавать и сразу submit-ить заказ, но доменная граница всё равно остаётся.

### Переходы MVP

| Transition | Actor | Prerequisites | Side effects | Failure cases |
|---|---|---|---|---|
| none → draft | client; manager от имени клиента | customer существует, базовые значения корректны | создаётся Order и history | CUSTOMER_NOT_FOUND, INVALID_CARGO |
| draft → ready_for_dispatch | owner-client; manager | endpoints и cargo заполнены; route/price рассчитаны; window валиден | фиксируются route/price snapshots | ORDER_INCOMPLETE, ROUTE_UNAVAILABLE |
| ready_for_dispatch → assigned | assigned driver через AcceptAssignment | текущий pending Assignment; offer не истёк; hard constraints валидны | Assignment accepted, Order assigned | ASSIGNMENT_STALE, ASSIGNMENT_EXPIRED |
| assigned → ready_for_dispatch | manager через release/reassign | Assignment ещё не active; есть reason | Assignment released, reservation освобождена | ASSIGNMENT_ALREADY_ACTIVE |
| assigned → in_transit | assigned driver; privileged manager override | Assignment accepted; ресурсы допустимы; loading завершён, если нужен | Assignment active, started_at, history | ASSIGNMENT_NOT_ACCEPTED, WRONG_DRIVER |
| in_transit → arrived | assigned driver; manager override | Assignment active | arrived_at | DELIVERY_NOT_STARTED, WRONG_DRIVER |
| arrived → completed | assigned driver с proof; manager override | proof policy выполнена | Order и Assignment completed | DELIVERY_PROOF_REQUIRED |
| draft → cancelled | owner-client; manager | reason | cancelled_at, history | ORDER_ALREADY_TERMINAL |
| ready_for_dispatch → cancelled | owner-client; manager | open Assignment освобождается | Assignment released | CANCELLATION_NOT_ALLOWED |
| assigned → cancelled | manager/admin | delivery не начата; reason | accepted Assignment released | DELIVERY_ALREADY_STARTED |

### Запрещённые переходы

    draft → assigned
    draft → in_transit
    ready_for_dispatch → in_transit
    ready_for_dispatch → completed
    assigned → completed
    in_transit → assigned
    in_transit → cancelled       // MVP
    arrived → cancelled          // MVP
    completed → любой статус
    cancelled → любой статус

Повтор команды с тем же command ID может быть идемпотентным. Обычный повтор без такого контекста должен возвращать доменный conflict, а не молча переписывать состояние.

## 3. Target state machine

    draft
      ├─ SubmitOrder → ready_for_dispatch
      └─ CancelOrder → cancelled

    ready_for_dispatch
      ├─ AssignmentAccepted fact → assigned
      └─ CancelOrder → cancelled

    assigned
      ├─ BeginLoading → loading
      ├─ StartDelivery → in_transit       // когда loading не моделируется
      ├─ ReleaseAssignment → ready_for_dispatch
      └─ CancelOrder → cancelled

    loading
      ├─ StartDelivery → in_transit
      ├─ CancelOrder → cancelled          // только с возвратом груза
      └─ AbortOrder → failed

    in_transit
      ├─ ConfirmArrival → arrived
      └─ AbortOrder → failed

    arrived
      ├─ ConfirmDelivery → completed
      └─ AbortOrder → failed

    completed / cancelled / failed
      └─ terminal

Rejected не является Order status: водитель отклоняет Assignment.

On-hold лучше моделировать отдельной сущностью order_holds. Hold ортогонален lifecycle: заказ может быть assigned и одновременно заблокирован погодой или отсутствием документа. Активный hold запрещает отдельные commands, не заменяя основной status.

Failed не нужен в MVP. В target он означает окончательно прекращённую перевозку. Временная поломка с последующей перегрузкой — incident/recovery, а не обязательно failed Order.

Terminal states: completed, cancelled и в target failed. Прямых переходов из них нет.

## 4. Commands вместо UpdateOrderStatus

Command выражает намерение и может завершиться ошибкой. Fact/event описывает уже произошедшее.

    Command: AcceptAssignment
    Fact: AssignmentAccepted

    Command: ConfirmArrival
    Fact: ArrivalConfirmed

    Telemetry fact: ArrivalDetected

ArrivalDetected от GPS не должен автоматически подтверждать прибытие: он может только предложить водителю выполнить ConfirmArrival.

### CreateOrder

Actor: client или manager от имени клиента.

Input:

    customer_id
    origin
    destination
    cargo_description
    weight_kg
    volume_m3
    pickup/delivery window
    special requirements

Агрегаты: новый Order.

Инварианты: actor имеет право создавать заказ; weight/volume корректны; locations заданы; начало window раньше конца.

Изменения: создаётся Order(draft).

Ошибки: CUSTOMER_NOT_FOUND, INVALID_CARGO, INVALID_LOCATION, INVALID_TIME_WINDOW.

### SubmitOrder

Actor: owner-client или manager.

Input: order_id, expected_order_version.

Агрегаты: Order; routing/pricing являются domain services.

Инварианты: Order в draft; обязательные данные заполнены; window актуален; route и price успешно рассчитаны.

Изменения: draft → ready_for_dispatch; route/price snapshots фиксируются.

Ошибки: INVALID_ORDER_TRANSITION, ORDER_INCOMPLETE, ROUTE_NOT_FOUND, PRICE_CALCULATION_FAILED, ORDER_STALE.

### ProposeAssignment

Название точнее AssignResources: водитель ещё не принял предложение.

Actor: manager; в будущем system actor Dispatch проходит тот же application use case.

Input:

    order_id
    driver_id
    vehicle_id
    planned_time_window
    expected_order_version
    recommendation_id?
    recommendation_snapshot_version?

Агрегаты: Order, новый Assignment; читаются Driver и Vehicle.

Инварианты:

- Order ready_for_dispatch;
- другого open Assignment нет;
- driver/vehicle eligible;
- driver available в window;
- driver и vehicle не зарезервированы;
- recommendation не является разрешением или reservation.

Изменения: создаётся Assignment(pending_acceptance), который резервирует ресурсы. Order не меняется.

Ошибки: INVALID_ORDER_TRANSITION, ACTIVE_ASSIGNMENT_EXISTS, DRIVER_NOT_ELIGIBLE, VEHICLE_CAPACITY_EXCEEDED, RESOURCE_ALREADY_RESERVED, RECOMMENDATION_STALE.

### AcceptAssignment

Actor: только driver из Assignment.

Input: assignment_id, expected_assignment_version.

Агрегаты: Assignment и Order; Driver/Vehicle перечитываются.

Инварианты: Assignment pending_acceptance; offer не истёк; actor совпадает; Order ready_for_dispatch; hard constraints всё ещё валидны.

Изменения: Assignment accepted; Order ready_for_dispatch → assigned.

Ошибки: WRONG_DRIVER, ASSIGNMENT_EXPIRED, ASSIGNMENT_STALE, INVALID_ASSIGNMENT_TRANSITION, DRIVER_LICENSE_EXPIRED.

### RejectAssignment

Actor: driver из Assignment.

Input: assignment_id, reason_code, comment?, expected_assignment_version.

Инварианты: Assignment ожидает ответ; actor совпадает.

Изменения: Assignment rejected; reservation освобождается; Order остаётся ready_for_dispatch.

Ошибки: WRONG_DRIVER, ASSIGNMENT_EXPIRED, INVALID_ASSIGNMENT_TRANSITION, ASSIGNMENT_STALE.

### ReassignResources

Отдельная атомарная команда лучше последовательности Release + Propose, которая может оставить заказ без назначения при частичном успехе.

Actor: manager.

Input: old_assignment_id, new driver/vehicle/window, reason, expected versions, optional recommendation_id.

Агрегаты: Order, старый Assignment, новый Assignment, Fleet resources.

Инварианты: старый Assignment pending_acceptance или accepted; доставка не началась; новые ресурсы проходят hard constraints; window свободен.

Изменения в одной transaction:

    old Assignment → released
    new Assignment → pending_acceptance

    Order:
      assigned → ready_for_dispatch, если старый был accepted
      ready_for_dispatch → без изменения, если старый ожидал ответа

Если новый ресурс нельзя зарезервировать, старый Assignment не освобождается.

Ошибки: ASSIGNMENT_ALREADY_ACTIVE, RESOURCE_ALREADY_RESERVED, DRIVER_NOT_ELIGIBLE, ASSIGNMENT_STALE.

### StartDelivery

Actor: assigned driver; manager override — отдельный privileged use case.

Input: order_id, assignment_id, started_at, expected versions.

Агрегаты: Order и Assignment; Driver/Vehicle проверяются повторно.

Инварианты: Order assigned; Assignment accepted; actor совпадает; vehicle operational; документы действуют; start window допустим; loading завершён, если требуется.

Изменения:

    Order.assigned → in_transit
    Assignment.accepted → active
    started_at = now

Ошибки: ASSIGNMENT_NOT_ACCEPTED, WRONG_DRIVER, START_TOO_EARLY, LOADING_NOT_COMPLETED, VEHICLE_NOT_OPERATIONAL.

### ConfirmArrival

Actor: assigned driver; manager override.

Input: order_id, optional coordinates, timestamp, expected version.

Инварианты: перевозка active; actor соответствует Assignment.

Изменения: in_transit → arrived; arrived_at.

Ошибки: DELIVERY_NOT_STARTED, WRONG_DRIVER, INVALID_ORDER_TRANSITION.

### ConfirmDelivery

Actor: assigned driver; manager override; в будущем recipient/customer confirmation.

Input:

    order_id
    recipient_name
    delivered_at
    proof_reference?
    comment?
    expected_order_version

Инварианты: Order arrived; Assignment active; proof policy выполнена; actor разрешён.

Изменения:

    Order.arrived → completed
    Assignment.active → completed
    completed_at

Ошибки: DELIVERY_PROOF_REQUIRED, WRONG_DRIVER, ASSIGNMENT_STALE, INVALID_ORDER_TRANSITION.

### CancelOrder

Actor:

- client-owner для draft и ready_for_dispatch;
- manager/admin для draft, ready_for_dispatch и assigned;
- после начала перевозки запрещено в MVP.

Input: order_id, reason_code, comment?, expected_version.

Агрегаты: Order и open Assignment.

Изменения: Order cancelled; Assignment released; reservations прекращаются.

Ошибки: CANCELLATION_NOT_ALLOWED, DELIVERY_ALREADY_STARTED, ORDER_ALREADY_TERMINAL, ORDER_STALE.

### Команды, которые не нужны

- RequestAssignment: SubmitOrder уже делает заказ готовым.
- UpdateStatus: удаляется.
- CompleteOrder: дублирует ConfirmDelivery.
- SetDriverBusy: busy вычисляется из Assignment.
- ApplyDispatchRecommendation: recommendation является входом ProposeAssignment, не authoritative действием.

## 5. Assignment как aggregate

Assignment должен быть отдельным aggregate внутри Core, а не вложенным entity Order и не простой persistence model.

Причины:

1. У Assignment собственный lifecycle.
2. Для одного Order будет несколько последовательных попыток.
3. Его concurrency затрагивает другие Orders через driver/vehicle.
4. Ему нужна собственная optimistic version.
5. Его можно reject/release без смены Order state.
6. Позже aggregate естественно переносится в Dispatch & Scheduling.

Application command может менять Order и Assignment в одной PostgreSQL transaction: внутри одного bounded context это нормально.

### Assignment lifecycle

    pending_acceptance
      ├─ AcceptAssignment → accepted
      ├─ RejectAssignment → rejected
      ├─ WithdrawAssignment → released
      └─ OfferExpired → expired

    accepted
      ├─ StartDelivery → active
      ├─ ReassignResources → released
      └─ CancelOrder → released

    active
      ├─ ConfirmDelivery → completed
      └─ target: AbortDelivery → terminated

    rejected / released / expired / completed / terminated
      └─ terminal

Для MVP достаточно:

    pending_acceptance
    accepted
    active
    rejected
    released
    expired
    completed

### Assignment fields

    id
    order_id
    driver_id
    vehicle_id

    status
    planned_time_window

    created_by_user_id
    source                         // manual | dispatch_recommendation
    recommendation_id?
    recommendation_created_at?

    assigned_at                    // offer created
    offer_expires_at?
    accepted_at?
    rejected_at?
    released_at?
    started_at?
    completed_at?

    rejection_reason_code?
    rejection_comment?
    release_reason_code?

    version
    created_at
    updated_at

Исторические display snapshots могут включать driver name/license и vehicle plate/description. Source of truth текущих данных остаётся Fleet; snapshots нужны для истории и документов.

### Assignment attempt

Отдельная assignment_attempt не нужна. Несколько Assignment records на один Order уже являются попытками:

    Order X
    ├─ Assignment 1 → rejected by Driver A
    ├─ Assignment 2 → released by manager
    └─ Assignment 3 → accepted/completed

Assignment offers стоит выделять только если позднее появятся параллельные предложения нескольким водителям. В MVP параллельные offers лучше запретить.

### Pending offer и reservation

Pending offer должен резервировать ресурсы, иначе одному водителю можно отправить конфликтующие рейсы.

Статусы pending_acceptance, accepted и active блокируют пересекающиеся окна. Pending offer имеет offer_expires_at.

    recommendation       → ничего не резервирует
    Assignment proposal  → резервирует

### Сценарии

Менеджер назначил, driver отказался:

    Order:       ready_for_dispatch
    Assignment:  pending_acceptance → rejected
    Reservation: освобождена

Переназначение до ответа: old pending → released, new → pending. При невозможности нового reservation старое предложение остаётся.

Переназначение после accept: разрешено только до StartDelivery и требует reason. Order возвращается assigned → ready_for_dispatch.

После StartDelivery reassignment в MVP запрещён. Нужна отдельная recovery/cargo custody модель.

При отмене Order open Assignment получает released с reason order_cancelled. Это не rejection.

## 6. Eligibility, availability и reservation

### Eligibility

Способность выполнить конкретный Order:

    Driver:
    - существует;
    - active;
    - license действует на весь planned window;
    - required qualification присутствует;
    - нет suspension.

    Vehicle:
    - существует;
    - operational;
    - документы действуют;
    - capacity покрывает cargo;
    - cargo capabilities соответствуют требованиям.

Eligibility зависит от заказа. Универсальное driver.is_eligible хранить нельзя.

### Availability

Может ли ресурс работать в нужное время независимо от других заказов:

    Driver:
    - on_duty или window покрывается сменой;
    - не в leave/unavailability;
    - не suspended.

    Vehicle:
    - operational;
    - не находится в maintenance window.

### Reservation / busy

Busy означает наличие reserving Assignment с пересекающимся интервалом. Оно вычисляется, а не переключается пользователем.

Текущий UpdateMyDriverStatus позволяет вручную поставить available/on_route/off_duty. В новой модели on_route производен от active Assignment и не является duty status.

### Ownership

| Характеристика | Source of truth | Хранение |
|---|---|---|
| Driver exists/active | drivers | persistent |
| Driver duty | drivers.duty_status или shift | declared persistent fact |
| Driver license | driver_documents | persistent |
| Driver qualification | driver_qualifications | persistent |
| Vehicle operational state | vehicles.operational_status | persistent |
| Vehicle capacity | vehicles | persistent |
| Vehicle documents | vehicle_documents | persistent |
| Cargo requirements | Order snapshot | persistent |
| Eligibility for Order | FleetEligibilityPolicy | computed |
| Availability in interval | shifts/unavailability | computed |
| Busy/reserved | open overlapping assignments | computed |
| Effective candidate | eligibility + availability + no overlap | computed |
| GPS position | будущий Tracking | не authoritative в Core |

### Driver MVP fields

    id
    user_id
    status             // active | suspended | archived
    duty_status        // on_duty | off_duty | unavailable
    rating
    home_warehouse_id?
    version
    created_at
    updated_at

Текущий vehicle_id у Driver не должен быть authoritative. Допустим preferred_vehicle_id, но машина рейса всегда фиксируется Assignment.

Driver documents:

    id
    driver_id
    document_type
    document_number
    valid_from
    valid_until
    verification_status
    verified_at
    version

### Vehicle MVP fields

    id
    plate_number
    brand
    model
    year
    capacity_kg
    capacity_m3
    operational_status
    version
    created_at
    updated_at

Operational statuses:

    operational
    maintenance
    out_of_service
    archived

Vehicle documents аналогичны driver_documents.

### Driver shifts

Если assignments всегда немедленные, сначала достаточно duty_status. Но при planned_time_window полезна простая модель:

    driver_shifts
    - id
    - driver_id
    - time_window
    - status             // planned | active | cancelled

Assignment window должен помещаться в действующую смену.

### Нужна ли resource_reservations

Для MVP нет. Assignment уже содержит resource IDs, time range и reserving status.

Отдельная таблица понадобится, когда один calendar учитывает assignment, maintenance, leave, transfer и temporary block.

## 7. Контракт Core и Dispatch

Правильный flow:

    Core
      → загружает Order
      → формирует fleet snapshots
      → Dispatch.Recommend(...)
      ← ranking + explanations

    Manager выбирает candidate

    Core
      → заново загружает authoritative Order/Driver/Vehicle
      → повторяет hard validation
      → пытается зарезервировать resources
      → создаёт Assignment

Dispatch может фильтровать, scoring/ranking и объяснять результат. Он не может гарантировать текущую свободу ресурса, валидность документа или создание Assignment.

Core повторно проверяет:

    order state/version
    driver status/documents/shift
    vehicle status/capacity/documents
    cargo restrictions
    resource overlap
    open assignment for order

Recommendation contract:

    recommendation_id
    order_id
    order_version
    calculated_at
    expires_at
    policy_version

    candidates:
    - driver_id
    - driver_version
    - vehicle_id
    - vehicle_version
    - score
    - explanations
    - observed_constraints

Versions помогают диагностировать RECOMMENDATION_STALE, но не заменяют повторную проверку.

### Race

    t0: Dispatch рекомендует Driver A для Order 1
    t1: Manager 2 резервирует Driver A для Order 2
    t2: Manager 1 назначает Driver A для Order 1

На t2 Core начинает transaction, перечитывает authoritative state и вставляет reserving Assignment. PostgreSQL exclusion constraint обнаруживает overlap. Transaction откатывается, API возвращает RESOURCE_ALREADY_RESERVED и предлагает пересчитать recommendation.

Recommendation никогда не является lock/reservation.

## 8. PostgreSQL model

### orders

Назначение: authoritative Order lifecycle.

Ключевые поля:

    id UUID PK
    customer_id UUID NOT NULL FK users
    status VARCHAR NOT NULL
    origin/destination IDs and snapshots
    cargo_description
    weight_kg NUMERIC
    volume_m3 NUMERIC
    pickup_window TSTZRANGE
    route_plan_id UUID?
    route_distance_km NUMERIC?
    quoted_price NUMERIC?
    currency CHAR(3)
    tariff_version_id UUID?
    submitted_at
    started_at
    arrived_at
    completed_at
    cancelled_at
    cancellation_reason_code
    version BIGINT NOT NULL DEFAULT 1
    created_at
    updated_at

Constraints:

    CHECK status IN (...)
    CHECK weight_kg > 0
    CHECK volume_m3 > 0
    CHECK NOT isempty(pickup_window)
    CHECK lower(pickup_window) < upper(pickup_window)
    CHECK quoted_price >= 0

Historical FK deletion лучше RESTRICT. Business resources архивируются вместо физического удаления.

Индексы:

    (customer_id, created_at DESC)
    (status, created_at)
    GIST (pickup_window)
    (origin_warehouse_id, status)

### assignments

Назначение: offers, confirmed assignments и reservation driver/vehicle.

    id UUID PK
    order_id UUID NOT NULL FK orders
    driver_id UUID NOT NULL FK drivers
    vehicle_id UUID NOT NULL FK vehicles
    status VARCHAR NOT NULL
    planned_time_window TSTZRANGE NOT NULL

    created_by_user_id UUID NOT NULL FK users
    source VARCHAR NOT NULL
    recommendation_id UUID?
    recommendation_created_at?

    assigned_at TIMESTAMPTZ NOT NULL
    offer_expires_at TIMESTAMPTZ
    accepted_at TIMESTAMPTZ
    rejected_at TIMESTAMPTZ
    released_at TIMESTAMPTZ
    started_at TIMESTAMPTZ
    completed_at TIMESTAMPTZ
    recipient_name VARCHAR?
    delivery_comment TEXT?

    rejection_reason_code VARCHAR
    rejection_comment TEXT
    release_reason_code VARCHAR

    version BIGINT NOT NULL DEFAULT 1
    created_at
    updated_at

Checks:

    status IN (
      pending_acceptance,
      accepted,
      active,
      rejected,
      released,
      expired,
      completed
    )

    NOT isempty(planned_time_window)
    lower(planned_time_window) < upper(planned_time_window)
    status != rejected OR rejected_at IS NOT NULL
    status != accepted OR accepted_at IS NOT NULL

Один open Assignment на Order:

    CREATE UNIQUE INDEX one_open_assignment_per_order
    ON assignments(order_id)
    WHERE status IN ('pending_acceptance', 'accepted', 'active');

Driver overlap guard:

    EXCLUDE USING gist (
      driver_id WITH =,
      planned_time_window WITH &&
    )
    WHERE (status IN ('pending_acceptance', 'accepted', 'active'));

Vehicle overlap guard аналогичен. Для UUID equality в GiST нужен btree_gist.

Дополнительные индексы:

    (order_id, assigned_at DESC)
    (driver_id, status)
    (vehicle_id, status)
    (offer_expires_at) WHERE status = pending_acceptance

### order_status_history

Immutable audit переходов:

    id UUID PK
    order_id UUID NOT NULL FK orders
    from_status
    to_status NOT NULL
    command_type NOT NULL
    actor_user_id UUID?
    actor_role
    reason_code?
    metadata JSONB?
    order_version BIGINT NOT NULL
    occurred_at TIMESTAMPTZ NOT NULL

Constraints:

    CHECK from_status IS DISTINCT FROM to_status
    UNIQUE(order_id, order_version)

Source of truth текущего status остаётся orders.status.

### drivers

    id UUID PK
    user_id UUID NOT NULL UNIQUE
    status VARCHAR NOT NULL
    duty_status VARCHAR NOT NULL
    rating NUMERIC
    home_warehouse_id UUID?
    version BIGINT NOT NULL DEFAULT 1
    timestamps

Checks:

    status IN ('active', 'suspended', 'archived')
    duty_status IN ('on_duty', 'off_duty', 'unavailable')
    rating BETWEEN 0 AND 5

### driver_documents

    id UUID PK
    driver_id UUID NOT NULL FK drivers
    document_type
    document_number
    valid_from
    valid_until
    verification_status
    verified_at
    version

Constraints:

    valid_until >= valid_from
    verification_status IN ('pending', 'verified', 'rejected', 'revoked')
    UNIQUE(driver_id, document_type, document_number)

### vehicles

    id UUID PK
    plate_number VARCHAR NOT NULL UNIQUE
    brand
    model
    year
    capacity_kg NUMERIC NOT NULL
    capacity_m3 NUMERIC NOT NULL
    operational_status VARCHAR NOT NULL
    version BIGINT NOT NULL DEFAULT 1
    timestamps

Checks:

    capacity_kg > 0
    capacity_m3 > 0
    operational_status IN (
      'operational',
      'maintenance',
      'out_of_service',
      'archived'
    )

vehicle_documents аналогична driver_documents.

SQL constraints не определяют actor permissions, loading requirements, cancellation policy, proof policy и qualification matching. Это остаётся в domain/application layer.

## 9. Concurrency для MVP

Нужна комбинация механизмов.

### SELECT FOR UPDATE

При изменении Order/Assignment:

    BEGIN

    SELECT order FOR UPDATE
    SELECT current assignment FOR UPDATE
    SELECT driver FOR UPDATE
    SELECT vehicle FOR UPDATE

    validate
    insert/update assignment
    update order
    insert status history

    COMMIT

Ресурсы всегда блокируются в одинаковом порядке: Driver, затем Vehicle. Команды изменения operational status должны соблюдать тот же порядок.

Row locking даёт стабильный eligibility snapshot, но сам по себе плохо выражает time ranges.

### Optimistic locking

orders.version и assignments.version защищают от stale UI:

    UPDATE orders
    SET status = $new_status,
        version = version + 1
    WHERE id = $id
      AND version = $expected_version;

Zero rows affected означает ORDER_STALE.

Order version не защищает от назначения одного Driver на два разных Orders.

### Partial unique

UNIQUE(driver_id) WHERE active слишком груб: он запрещает два непересекающихся будущих рейса.

### Exclusion constraint

Для Logiflow это главный authoritative guard:

    same driver + overlapping window → conflict
    same vehicle + overlapping window → conflict

Он работает между разными Orders и replicas.

### ProposeAssignment transaction

    1. BEGIN.
    2. Lock Order FOR UPDATE.
    3. Проверить expected version и ready_for_dispatch.
    4. Lock Driver и Vehicle FOR UPDATE.
    5. Перечитать documents/shift.
    6. Рассчитать eligibility.
    7. INSERT Assignment(pending_acceptance, planned_time_window).
    8. Exclusion constraints проверяют overlap.
    9. Записать остальные изменения.
    10. COMMIT.

Mapping DB conflicts:

    one_open_assignment_per_order violation
    → ACTIVE_ASSIGNMENT_EXISTS

    driver exclusion violation
    → RESOURCE_ALREADY_RESERVED {resourceType: driver}

    vehicle exclusion violation
    → RESOURCE_ALREADY_RESERVED {resourceType: vehicle}

Предварительный overlap SELECT полезен для сообщения, но не является защитой. Гарантия — exclusion constraint.

## 10. Domain API shape

Пример без полной реализации и без привязки к OpenAPI/pgx:

    type Order struct {
        ID           OrderID
        CustomerID   CustomerID
        Status       OrderStatus
        Cargo        Cargo
        Origin       Stop
        Destination  Stop
        PickupWindow TimeWindow

        RouteQuote *RouteQuoteSnapshot
        PriceQuote *PriceSnapshot

        StartedAt   *time.Time
        ArrivedAt   *time.Time
        CompletedAt *time.Time
        CancelledAt *time.Time
        Version     int64
    }

    func (o *Order) Submit(route RouteQuoteSnapshot, price PriceSnapshot, now time.Time) error
    func (o *Order) OnAssignmentAccepted(ref AssignmentRef, now time.Time) error
    func (o *Order) OnAssignmentReleased(id AssignmentID, now time.Time) error
    func (o *Order) StartDelivery(a Assignment, actor Actor, now time.Time) error
    func (o *Order) ConfirmArrival(actor Actor, now time.Time) error
    func (o *Order) Complete(proof DeliveryProof, actor Actor, now time.Time) error
    func (o *Order) Cancel(reason CancellationReason, actor Actor, now time.Time) error

Assignment:

    type Assignment struct {
        ID        AssignmentID
        OrderID   OrderID
        DriverID  DriverID
        VehicleID VehicleID

        Status     AssignmentStatus
        TimeWindow TimeWindow

        CreatedBy        UserID
        Source           AssignmentSource
        RecommendationID *RecommendationID

        AssignedAt     time.Time
        OfferExpiresAt *time.Time
        AcceptedAt     *time.Time
        RejectedAt     *time.Time
        ReleasedAt     *time.Time
        StartedAt      *time.Time
        CompletedAt    *time.Time

        RejectionReason *RejectionReason
        ReleaseReason   *ReleaseReason
        Version         int64
    }

    func NewAssignmentProposal(...) (*Assignment, error)
    func (a *Assignment) Accept(driver DriverID, now time.Time) error
    func (a *Assignment) Reject(driver DriverID, reason RejectionReason, now time.Time) error
    func (a *Assignment) Release(actor Actor, reason ReleaseReason, now time.Time) error
    func (a *Assignment) Activate(now time.Time) error
    func (a *Assignment) Complete(now time.Time) error
    func (a *Assignment) Expire(now time.Time) error

Eligibility:

    type FleetEligibilityPolicy interface {
        Check(
            order Order,
            driver DriverSnapshot,
            vehicle VehicleSnapshot,
            window TimeWindow,
            at time.Time,
        ) []EligibilityViolation
    }

Persistence в текущем монолите не получает отдельный repository layer. Application service использует существующий `pkg/storage.go` напрямую с `pgxpool.Pool` или `pgx.Tx`. `storage.GetOne(..., sb.ForUpdate())` применяется для блокировок, `storage.Create/Update/Delete` — для записи. PostgreSQL-specific constraints остаются в миграциях. Отдельные `OrderRepository`, `AssignmentRepository`, `FleetRepository`, `UnitOfWork` и `Transactor` сейчас не вводятся.

Application service:

    SubmitOrder
    ProposeAssignment
    AcceptAssignment
    RejectAssignment
    ReassignResources
    StartDelivery
    ConfirmArrival
    ConfirmDelivery
    CancelOrder

Handlers после рефакторинга только разбирают HTTP DTO, создают Actor, вызывают application command и переводят DomainError в HTTP response. Они не обращаются к generic storage напрямую.

## 11. Domain errors

    INVALID_ORDER_TRANSITION
    INVALID_ASSIGNMENT_TRANSITION
    ORDER_INCOMPLETE
    ORDER_ALREADY_TERMINAL
    ORDER_STALE

    ASSIGNMENT_STALE
    ACTIVE_ASSIGNMENT_EXISTS
    ASSIGNMENT_EXPIRED
    ASSIGNMENT_NOT_ACCEPTED
    ASSIGNMENT_ALREADY_ACTIVE
    WRONG_DRIVER

    DRIVER_NOT_FOUND
    DRIVER_NOT_ACTIVE
    DRIVER_NOT_AVAILABLE
    DRIVER_NOT_ELIGIBLE
    DRIVER_LICENSE_EXPIRED
    DRIVER_QUALIFICATION_MISSING

    VEHICLE_NOT_FOUND
    VEHICLE_NOT_OPERATIONAL
    VEHICLE_CAPACITY_EXCEEDED
    VEHICLE_DOCUMENT_EXPIRED
    VEHICLE_CAPABILITY_MISSING

    RESOURCE_ALREADY_RESERVED
    INVALID_TIME_WINDOW
    CANCELLATION_NOT_ALLOWED
    DELIVERY_ALREADY_STARTED
    DELIVERY_PROOF_REQUIRED
    LOADING_NOT_COMPLETED
    RECOMMENDATION_STALE

HTTP mapping:

    404 → *_NOT_FOUND
    409 → transition/stale/reservation conflicts
    422 → eligibility/capacity/document failures
    403 → actor/ownership errors
    503 → route/pricing dependency unavailable

## 12. Порядок рефакторинга

1. Зафиксировать transition matrix тестами.
2. Ввести domain Order и убрать прямое присваивание Order.Status.
3. Создать explicit application commands, временно оставив старый endpoint адаптером.
4. Перевести команды на существующий `pkg/storage.go` и единый порядок блокировок Order → Assignment → Driver(s по UUID) → Vehicle(s по UUID).
5. Добавить order_status_history.
6. Создать Assignment aggregate и assignments.
7. Зафиксировать vehicle_id внутри Assignment и не выводить машину рейса из drivers.vehicle_id.
8. Разделить driver administrative status и duty status.
9. Добавить документы, capacity rules и FleetEligibilityPolicy.
10. Добавить planned time window и PostgreSQL exclusion constraints.
11. Реализовать accept/reject/reassign lifecycle.
12. Перевести клиентов на отдельные command endpoints.
13. Удалить PATCH /orders/{id}/status.
14. Только после стабилизации Core подключать recommendation-only Dispatch.

Assignment не следует выносить в Dispatch до формирования корректной модели внутри Core. Сначала нужны state machine, concurrency и business invariants; перенос готового aggregate позднее будет значительно безопаснее.
