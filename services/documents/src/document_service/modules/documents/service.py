from io import BytesIO
from uuid import uuid4

import structlog
from reportlab.lib.pagesizes import A4
from reportlab.lib.utils import simpleSplit
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from document_service.models import Document, OutboxEvent
from document_service.modules.documents.schemas import (
    DeliveryCompleted,
    DocumentReady,
)

log = structlog.get_logger(component="documents.service")


def render_delivery_confirmation(event: DeliveryCompleted, font_path: str) -> bytes:
    output = BytesIO()
    pdfmetrics.registerFont(TTFont("DejaVu", font_path))
    pdf = canvas.Canvas(output, pagesize=A4)
    pdf.setTitle(f"Подтверждение доставки {event.order_id}")
    pdf.setFont("DejaVu", 16)
    pdf.drawString(42, 790, "Подтверждение доставки")
    pdf.setFont("DejaVu", 10)
    lines = [
        f"Заказ: {event.order_id}",
        f"Доставлено: {event.delivered_at:%Y-%m-%d %H:%M UTC}",
        f"Откуда: {event.origin_address}",
        f"Куда: {event.destination_address}",
        f"Груз: {event.cargo_description or 'не указан'}",
        f"Вес: {event.weight_kg:.2f} кг; объём: {event.volume_m3:.2f} м³",
        "Стоимость по заказу: "
        + (
            f"{event.total_price:.2f}"
            if event.total_price is not None
            else "не указана"
        ),
        f"Водитель: {event.driver_id}",
        f"Машина: {event.vehicle_id}",
        f"Принял: {event.recipient_name or 'не указан'}",
    ]
    y = 750
    for line in lines:
        for part in simpleSplit(line, "DejaVu", 10, A4[0] - 84):
            pdf.drawString(42, y, part)
            y -= 18
        y -= 10
    pdf.setFont("DejaVu", 8)
    pdf.drawString(
        42, 50, "Сформировано по данным Logiflow. Не является подписанным актом."
    )
    pdf.save()
    return output.getvalue()


async def accept_delivery(
    db: AsyncSession,
    event: DeliveryCompleted,
    font_path: str,
) -> Document:
    existing = await db.scalar(
        select(Document).where(
            Document.order_id == event.order_id,
            Document.type == "delivery_confirmation",
        )
    )
    if existing is not None:
        return existing

    document = Document(
        order_id=event.order_id,
        user_id=event.user_id,
        type="delivery_confirmation",
        title="Подтверждение доставки",
        pdf_bytes=render_delivery_confirmation(event, font_path),
    )
    db.add(document)
    await db.flush()

    outbox = OutboxEvent(id=uuid4(), subject="document.ready.v1", payload={})
    ready = DocumentReady(
        event_id=outbox.id,
        document_id=document.id,
        order_id=document.order_id,
        user_id=document.user_id,
        type=document.type,
    )
    outbox.payload = ready.model_dump(mode="json")
    db.add(outbox)
    await db.commit()

    log.info(
        "document_generated",
        document_id=str(document.id),
        order_id=str(document.order_id),
        user_id=str(document.user_id),
    )
    return document
