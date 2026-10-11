from datetime import UTC, datetime
from io import BytesIO
from uuid import uuid4

from pypdf import PdfReader

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


def test_confirmation_keeps_party_snapshot_and_long_receipt_fields(font_path):
    event = DeliveryCompleted(
        event_id=uuid4(),
        order_id=uuid4(),
        user_id=uuid4(),
        origin_address="Склад отправления",
        destination_address="Адрес получателя",
        cargo_description="Запчасти",
        weight_kg=120,
        volume_m3=1.5,
        delivered_at=datetime.now(UTC),
        recipient_name="Иван Петров",
        driver_id=uuid4(),
        vehicle_id=uuid4(),
        fields=[
            {
                "label": "Грузоотправитель",
                "value": "Тестовая организация / ИНН 7707083893",
            },
            {"label": "Замечания при приёмке", "value": "Подробное замечание. " * 450},
            {"label": "Непрерывная строка", "value": "Щ" * 1000},
            {"label": "Конец приёмки", "value": "Последняя строка подтверждения"},
        ],
    )
    pages = PdfReader(BytesIO(render_delivery_confirmation(event, font_path))).pages
    text = "\n".join(page.extract_text() for page in pages)
    assert len(pages) > 1
    assert "Тестовая организация / ИНН 7707083893" in text
    assert "Последняя строка подтверждения" in pages[-1].extract_text()
    assert text.count("Подробное") == 450
    assert text.count("замечание.") == 450
    assert text.count("Щ") == 1000
    for page in pages:

        def within_page(value, _cm, tm, _font, _size, page=page):
            if value.strip():
                assert 0 <= tm[4] < float(page.mediabox.width)
                assert 0 <= tm[5] < float(page.mediabox.height)

        page.extract_text(visitor_text=within_page)
