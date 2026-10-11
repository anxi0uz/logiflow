"""Black-box Core smoke against a running API and an isolated logiflow_test DB.

Run with:
  LOGIFLOW_E2E_BASE_URL=http://127.0.0.1:45761 \
  LOGIFLOW_E2E_DATABASE_URL=postgres://.../logiflow_test?sslmode=disable \
  uv run --with pytest --with httpx --with 'psycopg[binary]' \
    pytest -q tests/e2e/test_core_http_flow.py
"""

import os
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC, datetime, timedelta
from threading import Barrier
from urllib.parse import urlparse
from uuid import uuid4

import httpx
import psycopg
import pytest


def test_order_flow_and_concurrent_reservation():
    base_url = os.getenv("LOGIFLOW_E2E_BASE_URL")
    database_url = os.getenv("LOGIFLOW_E2E_DATABASE_URL")
    if not base_url or not database_url:
        pytest.skip("LOGIFLOW_E2E_BASE_URL and LOGIFLOW_E2E_DATABASE_URL are required")
    if urlparse(database_url).path != "/logiflow_test":
        pytest.fail("E2E test requires an isolated database named logiflow_test")

    db = psycopg.connect(database_url, autocommit=True)
    dispatch_database_url = os.getenv("LOGIFLOW_E2E_DISPATCH_DATABASE_URL")
    if dispatch_database_url:
        if urlparse(dispatch_database_url).path != "/logiflow_test":
            pytest.fail("Dispatch E2E connection must use logiflow_test")
        with psycopg.connect(dispatch_database_url, autocommit=True) as reader:
            assert reader.execute("SELECT count(*) FROM orders").fetchone() is not None
            with pytest.raises(psycopg.errors.InsufficientPrivilege):
                reader.execute("SELECT count(*) FROM users")
            with pytest.raises(psycopg.errors.InsufficientPrivilege):
                reader.execute("UPDATE orders SET status = status WHERE FALSE")
    http = httpx.Client(base_url=base_url, timeout=30)
    suffix = uuid4().hex[:12]
    user_ids = {}
    order_ids = []
    counterparty_ids = []
    warehouse_ids = [uuid4(), uuid4()]
    vehicle_id = uuid4()
    driver_ids = [uuid4(), uuid4()]
    manager_ids = [uuid4(), uuid4(), uuid4()]

    def call(method, path, token=None, body=None, expected=200):
        headers = {"Authorization": f"Bearer {token}"} if token else {}
        response = http.request(method, path, headers=headers, json=body)
        assert response.status_code == expected, (
            f"{method} {path}: expected {expected}, got {response.status_code}: {response.text}"
        )
        return response.json()

    try:
        call("GET", "/health/ready")
        for name in (
            "client",
            "admin",
            "manager1",
            "manager2",
            "manager3",
            "driver1",
            "driver2",
        ):
            email = f"{name}-{suffix}@e2e.test"
            call(
                "POST",
                "/auth/register",
                body={
                    "email": email,
                    "password": "smoke-password-123",
                    "fullName": name,
                },
            )
            user_ids[name] = db.execute(
                "SELECT id FROM users WHERE email = %s", (email,)
            ).fetchone()[0]
            role = (
                "admin"
                if name == "admin"
                else "manager"
                if name.startswith("manager")
                else "driver"
                if name.startswith("driver")
                else "client"
            )
            db.execute(
                "UPDATE users SET role = %s WHERE id = %s", (role, user_ids[name])
            )

        for i, warehouse_id in enumerate(warehouse_ids):
            lat, lon = (60.1699, 24.9384) if i == 0 else (60.2055, 24.9839)
            db.execute(
                "INSERT INTO warehouses(id, slug, name, address, city, latitude, longitude, status) "
                "VALUES (%s, %s, %s, %s, 'Helsinki', %s, %s, 'active')",
                (
                    warehouse_id,
                    f"e2e-{suffix}-{i}",
                    f"E2E {i}",
                    f"Test address {i}",
                    lat,
                    lon,
                ),
            )
        for i, manager_id in enumerate(manager_ids):
            db.execute(
                "INSERT INTO managers(id, user_id, warehouse_id, slug) VALUES (%s, %s, %s, %s)",
                (
                    manager_id,
                    user_ids[f"manager{i + 1}"],
                    warehouse_ids[1] if i == 2 else warehouse_ids[0],
                    f"e2e-manager-{suffix}-{i}",
                ),
            )
        db.execute(
            "INSERT INTO vehicles(id, plate_number, capacity_kg, capacity_m3, status, slug) "
            "VALUES (%s, %s, 5000, 30, 'available', %s)",
            (vehicle_id, f"E2E-{suffix[:8]}", f"e2e-vehicle-{suffix}"),
        )
        db.execute(
            "INSERT INTO vehicle_documents(id, vehicle_id, type, number, valid_until, status) "
            "VALUES (%s, %s, 'registration', %s, %s, 'valid')",
            (
                uuid4(),
                vehicle_id,
                f"REG-{suffix}",
                (datetime.now(UTC) + timedelta(days=365)).date(),
            ),
        )
        for i, driver_id in enumerate(driver_ids):
            db.execute(
                "INSERT INTO drivers(id, user_id, license_number, license_expiry, slug, status) "
                "VALUES (%s, %s, %s, %s, %s, 'available')",
                (
                    driver_id,
                    user_ids[f"driver{i + 1}"],
                    f"LICENSE-{suffix}-{i}",
                    (datetime.now(UTC) + timedelta(days=365)).date(),
                    f"e2e-driver-{suffix}-{i}",
                ),
            )

        tokens = {}
        for name in user_ids:
            login = call(
                "POST",
                "/auth/login",
                body={
                    "email": f"{name}-{suffix}@e2e.test",
                    "password": "smoke-password-123",
                },
            )
            tokens[name] = login["data"]["auth"]["access_token"]

        now = datetime.now(UTC)

        for index, name in enumerate(("Отправитель", "Получатель", "Перевозчик")):
            party = call(
                "POST",
                "/api/v1/counterparties",
                tokens["admin"] if index == 2 else tokens["client"],
                {
                    "kind": "organization",
                    "name": name,
                    "inn": "7707083893",
                    "address": "Тестовый адрес организации",
                    "contactName": "Иван Петров",
                    "phone": "+7 900 000-00-00",
                    "shared": index == 2,
                },
                201,
            )["data"]["counterparty"]
            counterparty_ids.append(party["id"])
        call(
            "GET",
            f"/api/v1/counterparties/{counterparty_ids[0]}",
            tokens["manager3"],
            expected=404,
        )
        call("GET", f"/api/v1/counterparties/{counterparty_ids[2]}", tokens["client"])
        call("GET", "/api/v1/counterparties", tokens["driver1"], expected=403)

        def create_and_submit(hours):
            start = now + timedelta(hours=hours)
            end = start + timedelta(hours=1)
            response = call(
                "POST",
                "/api/v1/orders",
                tokens["client"],
                {
                    "originWarehouseId": str(warehouse_ids[0]),
                    "destinationWarehouseId": str(warehouse_ids[1]),
                    "destinationAddress": "Test destination",
                    "weightKg": 100,
                    "volumeM3": 1,
                    "pickupFrom": start.isoformat(),
                    "pickupTo": end.isoformat(),
                },
                201,
            )
            order = response["data"]["order"]["order"]
            order_id = order["id"]
            order_ids.append(order_id)
            assert response["data"]["order"]["route"]["DistanceKm"] > 0
            edited = call(
                "PATCH",
                f"/api/v1/orders/{order_id}",
                tokens["client"],
                {
                    "weightKg": 150,
                },
            )["data"]["order"]
            assert edited["totalPrice"] > order["totalPrice"]
            assert order["destinationWarehouseId"] == str(warehouse_ids[1])
            assert order["originAddress"] == "Test address 0"
            assert order["destinationAddress"] == "Test address 1"
            assert order["transportPolicyVersion"] == 1
            missing = call(
                "POST",
                f"/api/v1/orders/{order_id}/submit",
                tokens["client"],
                expected=409,
            )
            assert "TRANSPORT_DETAILS_REQUIRED" in str(missing)
            call(
                "PUT",
                f"/api/v1/orders/{order_id}/transport-details",
                tokens["client"],
                {
                    "senderId": counterparty_ids[0],
                    "recipientId": counterparty_ids[1],
                    "carrierId": counterparty_ids[2],
                    "basisType": "transport_request",
                    "basisNumber": f"E2E-{suffix}",
                    "basisDate": now.date().isoformat(),
                    "packageCount": 5,
                    "terms": "Приёмка по фактическому количеству мест с фиксацией замечаний",
                },
            )
            call("POST", f"/api/v1/orders/{order_id}/submit", tokens["client"])
            receipt = {
                "operationType": "receive",
                "packageCount": 5,
                "cargoCondition": "Упаковка цела",
            }
            call(
                "POST",
                f"/api/v1/orders/{order_id}/warehouse-operations",
                tokens["client"],
                receipt,
                403,
            )
            call(
                "POST",
                f"/api/v1/orders/{order_id}/warehouse-operations",
                tokens["manager3"],
                receipt,
                403,
            )
            call(
                "POST",
                f"/api/v1/orders/{order_id}/warehouse-operations",
                tokens["manager1"],
                receipt,
                201,
            )
            return order_id, start, end

        def propose(
            order_id, start, end, driver_id, manager="manager1", recommendation=None
        ):
            body = {
                "driverId": str(driver_id),
                "vehicleId": str(vehicle_id),
                "plannedFrom": start.isoformat(),
                "plannedTo": end.isoformat(),
            }
            if recommendation:
                body.update(
                    {
                        "source": "dispatch_recommendation",
                        "recommendationId": recommendation["id"],
                        "recommendationCreatedAt": recommendation["createdAt"],
                    }
                )
            return call(
                "POST",
                f"/api/v1/orders/{order_id}/assignments",
                tokens[manager],
                body,
                201,
            )["data"]["assignment"]

        # One order: edit draft, submit, reject, reassign, deliver.
        order_id, start, end = create_and_submit(2)
        call(
            "GET",
            f"/api/v1/orders/{order_id}/dispatch-recommendations",
            tokens["client"],
            expected=403,
        )
        for _ in range(60):
            async_result = call(
                "GET",
                f"/api/v1/orders/{order_id}/dispatch-recommendations",
                tokens["manager1"],
            )["data"]["success"]
            if async_result["status"] == "ready":
                break
            time.sleep(0.2)
        assert async_result["status"] == "ready"
        assert async_result["candidates"]
        refreshed = call(
            "POST",
            f"/api/v1/orders/{order_id}/dispatch-recommendations",
            tokens["manager1"],
        )["data"]["success"]
        assert refreshed["status"] == "ready"
        assert refreshed["candidates"]
        first = propose(order_id, start, end, driver_ids[0])
        call(
            "POST",
            f"/api/v1/assignments/{first['id']}/accept",
            tokens["driver2"],
            expected=403,
        )
        call(
            "POST",
            f"/api/v1/assignments/{first['id']}/start",
            tokens["driver1"],
            expected=409,
        )
        rejected = call(
            "POST",
            f"/api/v1/assignments/{first['id']}/reject",
            tokens["driver1"],
            {
                "reasonCode": "unavailable",
            },
        )["data"]["assignment"]
        assert rejected["status"] == "rejected"
        call(
            "POST",
            f"/api/v1/assignments/{first['id']}/accept",
            tokens["driver1"],
            expected=409,
        )
        assert (
            call("GET", f"/api/v1/orders/{order_id}", tokens["client"])["data"][
                "success"
            ]["status"]
            == "ready_for_dispatch"
        )
        before_release = refreshed["createdAt"]
        for _ in range(60):
            refreshed = call(
                "GET",
                f"/api/v1/orders/{order_id}/dispatch-recommendations",
                tokens["manager1"],
            )["data"]["success"]
            if (
                refreshed["status"] == "ready"
                and refreshed["createdAt"] != before_release
            ):
                break
            time.sleep(0.2)
        assert refreshed["status"] == "ready"
        assert refreshed["createdAt"] != before_release
        candidate = next(
            c
            for c in refreshed["candidates"]
            if c["driver_id"] == str(driver_ids[1])
            and c["vehicle_id"] == str(vehicle_id)
        )
        candidate["createdAt"] = refreshed["createdAt"]
        second = propose(order_id, start, end, driver_ids[1], recommendation=candidate)
        assert second["source"] == "dispatch_recommendation"
        assert (
            call(
                "POST", f"/api/v1/assignments/{second['id']}/accept", tokens["driver2"]
            )["data"]["assignment"]["status"]
            == "accepted"
        )
        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["driver2"],
            expected=409,
        )

        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/start",
            tokens["driver2"],
            expected=409,
        )
        loading = {
            "operationType": "load",
            "assignmentId": second["id"],
            "packageCount": 5,
            "cargoCondition": "Погружено без замечаний",
        }
        call(
            "POST",
            f"/api/v1/orders/{order_id}/warehouse-operations",
            tokens["driver2"],
            loading,
            403,
        )
        call(
            "POST",
            f"/api/v1/orders/{order_id}/warehouse-operations",
            tokens["manager1"],
            loading,
            201,
        )
        call(
            "POST",
            f"/api/v1/orders/{order_id}/warehouse-operations",
            tokens["manager1"],
            loading,
            409,
        )
        queue = call(
            "GET", f"/api/v1/warehouses/{warehouse_ids[0]}/orders", tokens["manager1"]
        )["data"]["orders"]
        assert (
            next(item for item in queue if item["id"] == order_id)["warehouseStage"]
            == "ready_to_depart"
        )
        call(
            "GET",
            f"/api/v1/warehouses/{warehouse_ids[0]}/orders",
            tokens["manager3"],
            expected=403,
        )
        call("DELETE", f"/warehouses/e2e-{suffix}-0", tokens["manager1"], expected=403)
        blocked_delete = call(
            "DELETE", f"/warehouses/e2e-{suffix}-0", tokens["admin"], expected=409
        )
        assert blocked_delete["data"]["error"] == "WAREHOUSE_IN_USE"

        assert (
            call(
                "POST", f"/api/v1/assignments/{second['id']}/start", tokens["driver2"]
            )["data"]["assignment"]["status"]
            == "active"
        )
        call(
            "POST", f"/api/v1/orders/{order_id}/cancel", tokens["client"], expected=403
        )
        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/arrive",
            tokens["driver1"],
            expected=403,
        )
        # Two recorded in-transit handovers; neither changes the order lifecycle.
        started_at = call("GET", f"/api/v1/orders/{order_id}", tokens["client"])[
            "data"
        ]["success"]["startedAt"]
        previous = second["id"]
        for outgoing, incoming, driver_index in (
            ("driver2", "driver1", 0),
            ("driver1", "driver2", 1),
        ):
            handover_body = {
                "driverId": str(driver_ids[driver_index]),
                "vehicleId": str(vehicle_id),
                "plannedTo": end.isoformat(),
                "reason": "Смена водителя для продолжения рейса",
                "location": "Площадка отдыха, тестовый адрес",
                "cargoCondition": "Упаковка целая, замечаний нет",
            }
            call(
                "POST",
                f"/api/v1/orders/{order_id}/handovers",
                tokens["client"],
                handover_body,
                expected=403,
            )
            handover = call(
                "POST",
                f"/api/v1/orders/{order_id}/handovers",
                tokens["manager1"],
                handover_body,
                expected=201,
            )["data"]["handover"]
            hid = handover["id"]
            call("GET", f"/api/v1/orders/{order_id}/handovers", tokens[incoming])
            call(
                "POST",
                f"/api/v1/handovers/{hid}/accept",
                tokens[incoming],
                expected=409,
            )
            call(
                "POST",
                f"/api/v1/handovers/{hid}/confirm-release",
                tokens[incoming],
                expected=403,
            )
            call("POST", f"/api/v1/handovers/{hid}/confirm-release", tokens[outgoing])
            call(
                "POST",
                f"/api/v1/handovers/{hid}/accept",
                tokens["manager1"],
                expected=403,
            )
            handed = call("POST", f"/api/v1/handovers/{hid}/accept", tokens[incoming])[
                "data"
            ]["handover"]
            assert handed["status"] == "completed"
            call(
                "POST",
                f"/api/v1/handovers/{hid}/accept",
                tokens[incoming],
                expected=409,
            )
            call(
                "POST",
                f"/api/v1/assignments/{previous}/arrive",
                tokens[outgoing],
                expected=409,
            )
            assert (
                db.execute(
                    "SELECT status FROM assignments WHERE id=%s", (previous,)
                ).fetchone()[0]
                == "released"
            )
            current = call("GET", f"/api/v1/orders/{order_id}", tokens["client"])[
                "data"
            ]["success"]
            assert (
                current["status"] == "in_transit" and current["startedAt"] == started_at
            )
            previous = handed["nextAssignmentId"]
        history = call(
            "GET", f"/api/v1/orders/{order_id}/handovers", tokens["manager1"]
        )["data"]["handovers"]
        assert len(history) == 2
        for endpoint in ("/me/trips", "/api/v1/orders"):
            former_trips = call("GET", endpoint, tokens["driver1"])["data"]["success"]
            assert any(item["id"] == order_id for item in former_trips)
        second = {**second, "id": previous}
        call("POST", f"/api/v1/assignments/{second['id']}/arrive", tokens["driver2"])
        incoming = call("GET", f"/api/v1/orders/{order_id}", tokens["manager3"])[
            "data"
        ]["success"]
        assert incoming["status"] == "arrived"
        queue = call(
            "GET", f"/api/v1/warehouses/{warehouse_ids[1]}/orders", tokens["manager3"]
        )["data"]["orders"]
        assert (
            next(item for item in queue if item["id"] == order_id)["warehouseStage"]
            == "awaiting_unloading"
        )
        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["driver2"],
            expected=403,
        )
        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["manager1"],
            expected=403,
        )
        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["manager3"],
            expected=409,
        )
        refusal = call(
            "POST",
            f"/api/v1/orders/{order_id}/incidents",
            tokens["driver2"],
            {
                "kind": "recipient_refusal",
                "description": "Получатель отказался от приёмки",
            },
            201,
        )["data"]["incident"]
        acceptance = {
            "resolution": "recipient_accepted",
            "note": "После согласования получатель согласился принять груз с замечаниями",
            "recipientName": "Recipient",
        }
        for name in ("client", "driver2", "manager1"):
            call(
                "POST",
                f"/api/v1/incidents/{refusal['id']}/resolve",
                tokens[name],
                acceptance,
                403,
            )
        call(
            "POST",
            f"/api/v1/incidents/{refusal['id']}/resolve",
            tokens["manager3"],
            acceptance,
            409,
        )
        unloading = {
            "operationType": "unload",
            "assignmentId": second["id"],
            "packageCount": 4,
            "cargoCondition": "Принято с недостачей одного места",
        }
        call(
            "POST",
            f"/api/v1/orders/{order_id}/warehouse-operations",
            tokens["manager3"],
            unloading,
            201,
        )
        journal = call(
            "GET", f"/api/v1/orders/{order_id}/warehouse-operations", tokens["client"]
        )["data"]["operations"]
        assert [item["operationType"] for item in journal] == [
            "receive",
            "load",
            "unload",
        ]
        incidents = call(
            "GET", f"/api/v1/orders/{order_id}/incidents", tokens["client"]
        )["data"]["incidents"]
        assert len(incidents) == 2
        shortage = next(
            item for item in incidents if item["kind"] == "quantity_discrepancy"
        )
        assert (shortage["expectedPackages"], shortage["actualPackages"]) == (5, 4)
        blocked = call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["manager3"],
            expected=409,
        )
        assert "INCIDENT_OPEN" in str(blocked)
        queue = call(
            "GET", f"/api/v1/warehouses/{warehouse_ids[1]}/orders", tokens["manager3"]
        )["data"]["orders"]
        held = next(item for item in queue if item["id"] == order_id)
        assert (
            held["warehouseStage"] == "incident_open" and held["openIncidentCount"] == 2
        )
        call(
            "POST",
            f"/api/v1/incidents/{refusal['id']}/resolve",
            tokens["manager3"],
            acceptance,
        )
        call(
            "POST",
            f"/api/v1/incidents/{shortage['id']}/resolve",
            tokens["manager3"],
            {
                "resolution": "accept_discrepancy",
                "note": "Недостача зафиксирована, претензия передана ответственному",
            },
        )
        missing = call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["manager3"],
            expected=409,
        )
        assert "DELIVERY_RECEIPT_REQUIRED" in str(missing)
        receipt = {
            "assignmentId": second["id"],
            "recipientName": "Recipient",
            "recipientRole": "Кладовщик",
            "authorityBasis": "Приёмка представителем склада, со слов менеджера",
            "packageCount": 4,
            "cargoCondition": "Принято с недостачей одного места",
            "sealNumber": "TEST-SEAL-1",
            "comment": "Недостача зафиксирована",
        }
        for name in ("client", "driver2", "manager1"):
            call(
                "POST",
                f"/api/v1/orders/{order_id}/delivery-receipt",
                tokens[name],
                receipt,
                403,
            )
        for invalid_receipt in (
            {key: value for key, value in receipt.items() if key != "packageCount"},
            {**receipt, "packageCount": None},
        ):
            call(
                "POST",
                f"/api/v1/orders/{order_id}/delivery-receipt",
                tokens["manager3"],
                invalid_receipt,
                400,
            )
        call(
            "POST",
            f"/api/v1/orders/{order_id}/delivery-receipt",
            tokens["manager3"],
            {**receipt, "packageCount": 5},
            422,
        )
        saved_receipt = call(
            "POST",
            f"/api/v1/orders/{order_id}/delivery-receipt",
            tokens["manager3"],
            receipt,
            201,
        )["data"]["receipt"]
        assert saved_receipt["recordedByUserId"] == str(user_ids["manager3"])
        assert (
            call(
                "GET", f"/api/v1/orders/{order_id}/delivery-receipt", tokens["client"]
            )["data"]["receipt"]["id"]
            == saved_receipt["id"]
        )
        call(
            "POST",
            f"/api/v1/orders/{order_id}/delivery-receipt",
            tokens["manager3"],
            receipt,
            409,
        )
        completed = call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["manager3"],
            {
                "recipientName": "Recipient",
                "comment": "Received",
            },
        )["data"]["assignment"]
        assert completed["status"] == "completed"
        assert db.execute(
            "SELECT status, recipient_name, delivery_comment FROM assignments WHERE id = %s",
            (second["id"],),
        ).fetchone() == ("completed", "Recipient", "Received")
        assert (
            call("GET", f"/api/v1/orders/{order_id}", tokens["client"])["data"][
                "success"
            ]["status"]
            == "completed"
        )
        call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["driver2"],
            expected=409,
        )

        if os.getenv("LOGIFLOW_E2E_DOCUMENTS") == "1":
            document = None
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                inbox = (
                    call("GET", "/api/v1/documents", tokens["client"])["data"][
                        "success"
                    ]
                    or []
                )
                document = next(
                    (
                        item
                        for item in inbox
                        if item["order_id"] == order_id
                        and item["type"] == "delivery_confirmation"
                    ),
                    None,
                )
                if document:
                    break
                time.sleep(0.25)
            assert document, "delivery confirmation was not generated"
            document_id = document["id"]
            assert document["type"] == "delivery_confirmation"
            owner_pdf = http.get(
                f"/api/v1/documents/{document_id}/download",
                headers={"Authorization": f"Bearer {tokens['client']}"},
            )
            assert owner_pdf.status_code == 200
            assert owner_pdf.headers["content-type"] == "application/pdf"
            assert owner_pdf.content.startswith(b"%PDF-")
            stranger_pdf = http.get(
                f"/api/v1/documents/{document_id}/download",
                headers={"Authorization": f"Bearer {tokens['driver1']}"},
            )
            assert stranger_pdf.status_code == 404

            linked_notice = False
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                notices = (
                    call("GET", "/notifications", tokens["client"])["data"]["success"]
                    or []
                )
                linked_notice = any(
                    item["DocumentID"] == document_id for item in notices
                )
                if linked_notice:
                    break
                time.sleep(0.25)
            assert linked_notice, "document notification was not delivered"

            expected_types = {
                "transport_order",
                "trip_assignment",
                "cargo_handover",
                "delivery_confirmation",
                "service_report",
            }
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                issued = (
                    call("GET", "/api/v1/documents?limit=100", tokens["client"])[
                        "data"
                    ]["success"]
                    or []
                )
                issued = [item for item in issued if item["order_id"] == order_id]
                if {item["type"] for item in issued} >= expected_types and sum(
                    item["type"] == "cargo_handover" for item in issued
                ) == 2:
                    break
                time.sleep(0.25)
            assert {item["type"] for item in issued} >= expected_types
            assert sum(item["type"] == "cargo_handover" for item in issued) == 2
            assert sum(item["type"] == "trip_assignment" for item in issued) == 3
            transfer_doc = next(
                item for item in issued if item["type"] == "cargo_handover"
            )
            for role in ("driver1", "driver2", "manager1"):
                assert (
                    http.get(
                        f"/api/v1/documents/{transfer_doc['id']}/download",
                        headers={"Authorization": f"Bearer {tokens[role]}"},
                    ).status_code
                    == 200
                )
            assert (
                http.get(
                    f"/api/v1/documents/{transfer_doc['id']}/download",
                    headers={"Authorization": f"Bearer {tokens['manager2']}"},
                ).status_code
                == 404
            )

        # Cancelling an open offer releases its reservation.
        cancel_id, start, end = create_and_submit(5)
        offer = propose(cancel_id, start, end, driver_ids[0])
        assert (
            call(
                "POST",
                f"/api/v1/orders/{cancel_id}/cancel",
                tokens["client"],
                {
                    "reasonCode": "not_needed",
                },
            )["data"]["order"]["status"]
            == "cancelled"
        )
        assert (
            db.execute(
                "SELECT status FROM assignments WHERE id = %s", (offer["id"],)
            ).fetchone()[0]
            == "released"
        )
        call(
            "POST",
            f"/api/v1/assignments/{offer['id']}/accept",
            tokens["driver1"],
            expected=409,
        )

        # Cargo remains at the origin after cancellation until physically returned.
        queue = call(
            "GET", f"/api/v1/warehouses/{warehouse_ids[0]}/orders", tokens["manager1"]
        )["data"]["orders"]
        assert (
            next(item for item in queue if item["id"] == cancel_id)["warehouseStage"]
            == "awaiting_return"
        )
        call(
            "POST",
            f"/api/v1/orders/{cancel_id}/warehouse-operations",
            tokens["manager1"],
            {
                "operationType": "return_to_sender",
                "packageCount": 5,
                "cargoCondition": "Упаковка цела",
                "comment": "Выдано представителю отправителя после отмены",
                "recipientName": "Sender",
            },
            201,
        )
        queue = call(
            "GET", f"/api/v1/warehouses/{warehouse_ids[0]}/orders", tokens["manager1"]
        )["data"]["orders"]
        assert all(item["id"] != cancel_id for item in queue)

        if os.getenv("LOGIFLOW_E2E_DOCUMENTS") == "1":
            deadline = time.monotonic() + 15
            cancelled_doc = None
            while time.monotonic() < deadline:
                inbox = (
                    call("GET", "/api/v1/documents?limit=100", tokens["client"])[
                        "data"
                    ]["success"]
                    or []
                )
                cancelled_doc = next(
                    (
                        item
                        for item in inbox
                        if item["order_id"] == cancel_id
                        and item["type"] == "cancellation_notice"
                    ),
                    None,
                )
                if cancelled_doc:
                    break
                time.sleep(0.25)
            assert cancelled_doc, "cancellation notice was not generated"

        # Undoing actual loading frees the reservation and preserves both records.
        undo_id, start, end = create_and_submit(6)
        loaded_offer = propose(undo_id, start, end, driver_ids[0])
        call(
            "POST",
            f"/api/v1/assignments/{loaded_offer['id']}/accept",
            tokens["driver1"],
        )
        call(
            "POST",
            f"/api/v1/orders/{undo_id}/warehouse-operations",
            tokens["manager1"],
            {
                "operationType": "load",
                "assignmentId": loaded_offer["id"],
                "packageCount": 5,
                "cargoCondition": "Целый",
            },
            201,
        )
        breakdown = call(
            "POST",
            f"/api/v1/orders/{undo_id}/incidents",
            tokens["driver1"],
            {
                "kind": "vehicle_breakdown",
                "description": "Машина неисправна перед выездом",
            },
            201,
        )["data"]["incident"]
        call(
            "POST",
            f"/api/v1/assignments/{loaded_offer['id']}/start",
            tokens["driver1"],
            expected=409,
        )
        call(
            "POST",
            f"/api/v1/orders/{undo_id}/warehouse-operations",
            tokens["manager1"],
            {
                "operationType": "undo_load",
                "assignmentId": loaded_offer["id"],
                "packageCount": 5,
                "cargoCondition": "Целый",
                "comment": "Груз снят с неисправной машины",
            },
            201,
        )
        assert (
            call("GET", f"/api/v1/orders/{undo_id}", tokens["client"])["data"][
                "success"
            ]["status"]
            == "ready_for_dispatch"
        )
        assert db.execute(
            "SELECT status FROM assignments WHERE id=%s", (loaded_offer["id"],)
        ).fetchone() == ("released",)
        call(
            "POST",
            f"/api/v1/incidents/{breakdown['id']}/resolve",
            tokens["manager1"],
            {
                "resolution": "repaired",
                "note": "Ремонт выполнен, повторный рейс согласован",
            },
        )
        retry_offer = propose(undo_id, start, end, driver_ids[0])
        call(
            "POST", f"/api/v1/assignments/{retry_offer['id']}/accept", tokens["driver1"]
        )
        call(
            "POST",
            f"/api/v1/assignments/{retry_offer['id']}/start",
            tokens["driver1"],
            expected=409,
        )
        call("POST", f"/api/v1/orders/{undo_id}/cancel", tokens["manager1"])
        call(
            "POST",
            f"/api/v1/orders/{undo_id}/warehouse-operations",
            tokens["manager1"],
            {
                "operationType": "return_to_sender",
                "packageCount": 5,
                "cargoCondition": "Целый",
                "comment": "Отправитель забрал груз",
                "recipientName": "Sender",
            },
            201,
        )
        journal = call(
            "GET", f"/api/v1/orders/{undo_id}/warehouse-operations", tokens["client"]
        )["data"]["operations"]
        assert [item["operationType"] for item in journal] == [
            "receive",
            "load",
            "undo_load",
            "return_to_sender",
        ]

        # Two managers, two orders, the same driver and vehicle, concurrent Assign.
        one, start, end = create_and_submit(8)
        two, _, _ = create_and_submit(8)
        barrier = Barrier(2)

        def race(order_id, manager):
            barrier.wait(timeout=10)
            return http.post(
                f"/api/v1/orders/{order_id}/assignments",
                headers={"Authorization": f"Bearer {tokens[manager]}"},
                json={
                    "driverId": str(driver_ids[0]),
                    "vehicleId": str(vehicle_id),
                    "plannedFrom": start.isoformat(),
                    "plannedTo": end.isoformat(),
                },
            )

        with ThreadPoolExecutor(max_workers=2) as executor:
            futures = [
                executor.submit(race, one, "manager1"),
                executor.submit(race, two, "manager2"),
            ]
            responses = [future.result(timeout=30) for future in futures]
        assert sorted(response.status_code for response in responses) == [201, 409], [
            (response.status_code, response.text) for response in responses
        ]
        assert (
            db.execute(
                "SELECT count(*) FROM assignments WHERE order_id IN (%s, %s) "
                "AND status IN ('pending_acceptance', 'accepted', 'active')",
                (one, two),
            ).fetchone()[0]
            == 1
        )
        statuses = [
            call("GET", f"/api/v1/orders/{order_id}", tokens["client"])["data"][
                "success"
            ]["status"]
            for order_id in (one, two)
        ]
        assert statuses == ["ready_for_dispatch", "ready_for_dispatch"]

    finally:
        for order_id in order_ids:
            db.execute("DELETE FROM delivery_receipts WHERE order_id = %s", (order_id,))
            db.execute("DELETE FROM order_incidents WHERE order_id = %s", (order_id,))
            db.execute(
                "DELETE FROM warehouse_operations WHERE order_id = %s", (order_id,)
            )
            db.execute("DELETE FROM trip_handovers WHERE order_id = %s", (order_id,))
            db.execute("DELETE FROM orders WHERE id = %s", (order_id,))
        for driver_id in driver_ids:
            db.execute("DELETE FROM drivers WHERE id = %s", (driver_id,))
        db.execute("DELETE FROM vehicles WHERE id = %s", (vehicle_id,))
        for manager_id in manager_ids:
            db.execute("DELETE FROM managers WHERE id = %s", (manager_id,))
        for warehouse_id in warehouse_ids:
            db.execute("DELETE FROM warehouses WHERE id = %s", (warehouse_id,))
        for user_id in user_ids.values():
            db.execute(
                "DELETE FROM counterparties WHERE owner_user_id = %s", (user_id,)
            )
            db.execute("DELETE FROM users WHERE id = %s", (user_id,))
        http.close()
        db.close()
