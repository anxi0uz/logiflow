"""Black-box Core smoke against a running API and an isolated logiflow_test DB.

Run with:
  LOGIFLOW_E2E_BASE_URL=http://127.0.0.1:45761 \
  LOGIFLOW_E2E_DATABASE_URL=postgres://.../logiflow_test?sslmode=disable \
  uv run --with pytest --with httpx --with 'psycopg[binary]' \
    pytest -q tests/e2e/test_core_http_flow.py
"""

import os
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
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
    http = httpx.Client(base_url=base_url, timeout=30)
    suffix = uuid4().hex[:12]
    user_ids = {}
    order_ids = []
    warehouse_ids = [uuid4(), uuid4()]
    vehicle_id = uuid4()
    driver_ids = [uuid4(), uuid4()]
    manager_ids = [uuid4(), uuid4()]

    def call(method, path, token=None, body=None, expected=200):
        headers = {"Authorization": f"Bearer {token}"} if token else {}
        response = http.request(method, path, headers=headers, json=body)
        assert response.status_code == expected, (
            f"{method} {path}: expected {expected}, got {response.status_code}: {response.text}"
        )
        return response.json()

    try:
        call("GET", "/health/ready")
        for name in ("client", "manager1", "manager2", "driver1", "driver2"):
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
                "manager"
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
                "INSERT INTO warehouses(id, slug, name, address, city, latitude, longitude) "
                "VALUES (%s, %s, %s, %s, 'Helsinki', %s, %s)",
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
                    warehouse_ids[0],
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
                (datetime.now(timezone.utc) + timedelta(days=365)).date(),
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
                    (datetime.now(timezone.utc) + timedelta(days=365)).date(),
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

        now = datetime.now(timezone.utc)

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
            call("POST", f"/api/v1/orders/{order_id}/submit", tokens["client"])
            return order_id, start, end

        def propose(order_id, start, end, driver_id, manager="manager1"):
            return call(
                "POST",
                f"/api/v1/orders/{order_id}/assignments",
                tokens[manager],
                {
                    "driverId": str(driver_id),
                    "vehicleId": str(vehicle_id),
                    "plannedFrom": start.isoformat(),
                    "plannedTo": end.isoformat(),
                },
                201,
            )["data"]["assignment"]

        # One order: edit draft, submit, reject, reassign, deliver.
        order_id, start, end = create_and_submit(2)
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
        second = propose(order_id, start, end, driver_ids[1])
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
        call("POST", f"/api/v1/assignments/{second['id']}/arrive", tokens["driver2"])
        completed = call(
            "POST",
            f"/api/v1/assignments/{second['id']}/complete",
            tokens["driver2"],
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
            db.execute("DELETE FROM orders WHERE id = %s", (order_id,))
        for driver_id in driver_ids:
            db.execute("DELETE FROM drivers WHERE id = %s", (driver_id,))
        db.execute("DELETE FROM vehicles WHERE id = %s", (vehicle_id,))
        for manager_id in manager_ids:
            db.execute("DELETE FROM managers WHERE id = %s", (manager_id,))
        for warehouse_id in warehouse_ids:
            db.execute("DELETE FROM warehouses WHERE id = %s", (warehouse_id,))
        for user_id in user_ids.values():
            db.execute("DELETE FROM users WHERE id = %s", (user_id,))
        http.close()
        db.close()
