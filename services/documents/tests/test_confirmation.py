from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

from document_service.modules.documents.schemas import DeliveryCompleted
from document_service.modules.documents.service import render_delivery_confirmation


def test_delivery_confirmation_is_a_pdf():
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

    font = next(
        path
        for path in (
            Path("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"),
            Path("/usr/share/fonts/TTF/DejaVuSans.ttf"),
        )
        if path.exists()
    )
    pdf = render_delivery_confirmation(event, str(font))

    assert pdf.startswith(b"%PDF-")
    assert len(pdf) > 2000
