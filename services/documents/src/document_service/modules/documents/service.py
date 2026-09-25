from datetime import UTC
from io import BytesIO
from pathlib import Path
from uuid import uuid4

import structlog
from reportlab.lib.colors import HexColor
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
    condensed_path = Path(font_path).with_name("DejaVuSansCondensed-Bold.ttf")
    pdfmetrics.registerFont(TTFont("LogiflowBody", font_path))
    pdfmetrics.registerFont(
        TTFont(
            "LogiflowCondensed",
            str(condensed_path if condensed_path.exists() else font_path),
        )
    )
    pdf = canvas.Canvas(output, pagesize=A4)
    pdf.setTitle(f"Подтверждение доставки {event.order_id}")
    width, height = A4
    accent = HexColor("#243B63")
    ink = HexColor("#303846")
    divider = HexColor("#D9DEE5")
    left, right = 58, width - 54

    def page() -> None:
        pdf.setFillColor(accent)
        pdf.rect(0, 0, 18, height, fill=1, stroke=0)
        pdf.setFillColor(ink)
        pdf.setFont("LogiflowCondensed", 10)
        pdf.drawRightString(right, height - 56, "LOGIFLOW")

    def label(text: str, x: float, y: float) -> None:
        pdf.setFillColor(accent)
        pdf.setFont("LogiflowCondensed", 9)
        pdf.drawString(x, y, text.upper())

    def lines(value: str, available: float) -> list[str]:
        return simpleSplit(value, "LogiflowBody", 10.5, available) or [""]

    def field(text: str, value: str, x: float, y: float, available: float) -> float:
        label(text, x, y)
        pdf.setFillColor(ink)
        pdf.setFont("LogiflowBody", 10.5)
        for line in lines(value, available):
            y -= 19
            pdf.drawString(x, y, line)
        return y - 11

    def rule(y: float) -> float:
        pdf.setStrokeColor(divider)
        pdf.setLineWidth(0.6)
        pdf.line(left, y, right, y)
        return y - 36

    def ensure_room(y: float, needed: float) -> float:
        if y - needed < 62:
            pdf.showPage()
            page()
            return height - 105
        return y

    page()
    pdf.setFillColor(accent)
    title_size = min(
        40,
        40
        * (right - left)
        / pdfmetrics.stringWidth("ПОДТВЕРЖДЕНИЕ", "LogiflowCondensed", 40),
    )
    pdf.setFont("LogiflowCondensed", title_size)
    pdf.drawString(left, height - 128, "ПОДТВЕРЖДЕНИЕ")
    pdf.drawString(left, height - 178, "ДОСТАВКИ")

    y = height - 244
    delivered = event.delivered_at
    if delivered.tzinfo is not None:
        delivered = delivered.astimezone(UTC)
    delivered_text = delivered.strftime("%d.%m.%Y / %H:%M")
    if delivered.tzinfo is not None:
        delivered_text += " UTC"
    left_bottom = field("Заказ", str(event.order_id), left, y, 260)
    right_bottom = field("Доставлено", delivered_text, 350, y, right - 350)
    y = rule(min(left_bottom, right_bottom) - 13)

    route_width = (right - left - 34) / 2
    route_height = (
        max(
            len(lines(event.origin_address, route_width)),
            len(lines(event.destination_address, route_width)),
        )
        * 19
        + 44
    )
    y = ensure_room(y, route_height + 33)
    from_bottom = field("Откуда", event.origin_address, left, y, route_width)
    to_bottom = field(
        "Куда", event.destination_address, left + route_width + 34, y, route_width
    )
    y = rule(min(from_bottom, to_bottom) - 13)

    description = event.cargo_description
    y = ensure_room(y, len(lines(description, right - left)) * 19 + 116)
    y = field("Груз", description, left, y, right - left) - 20
    measure_width = (right - left - 40) / 3
    bottoms = [
        field("Вес", f"{event.weight_kg:.2f} кг", left, y, measure_width),
        field(
            "Объём",
            f"{event.volume_m3:.2f} м³",
            left + measure_width + 20,
            y,
            measure_width,
        ),
    ]
    if event.total_price is not None:
        bottoms.append(
            field(
                "Стоимость по заказу",
                f"{event.total_price:.2f}",
                left + 2 * (measure_width + 20),
                y,
                measure_width,
            )
        )
    y = rule(min(bottoms) - 13)

    details = [
        ("ID водителя", str(event.driver_id)),
        ("ID машины", str(event.vehicle_id)),
    ]
    if event.recipient_name:
        details.insert(0, ("Принял", event.recipient_name))
    for title, value in details:
        y = ensure_room(y, len(lines(value, right - left)) * 19 + 37)
        y = field(title, value, left, y, right - left) - 13

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
