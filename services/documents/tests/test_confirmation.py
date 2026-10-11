from datetime import UTC, datetime
from uuid import uuid4

from document_service.modules.documents.schemas import DeliveryCompleted
from document_service.modules.documents.service import render_delivery_confirmation


def test_delivery_confirmation_is_a_pdf(font_path):
    event = DeliveryCompleted(
        event_id=uuid4(),
        order_id=uuid4(),
        user_id=uuid4(),
        origin_address="Склад, Хельсинки",
        destination_address="Улица 1",
        cargo_description="Запчасти",
        weight_kg=120,
        volume_m3=1.5,
        total_price=950,
        delivered_at=datetime.now(UTC),
        recipient_name="Иван Иванов",
        driver_id=uuid4(),
        vehicle_id=uuid4(),
    )

    pdf = render_delivery_confirmation(event, font_path)

    assert pdf.startswith(b"%PDF-")
    assert len(pdf) > 2000
