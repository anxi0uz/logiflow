from io import BytesIO
from xml.sax.saxutils import escape

from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import HRFlowable, Paragraph, SimpleDocTemplate, Spacer
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from document_service.models import Document
from document_service.modules.documents.schemas import DocumentRequested
from document_service.modules.documents.service import add_ready_event

TITLES = {
    "transport_order": "Карточка заявки на перевозку",
    "trip_assignment": "Рейсовое задание",
    "cargo_handover": "Протокол передачи груза",
    "service_report": "Отчет о выполненной перевозке",
    "cancellation_notice": "Уведомление об отмене заказа",
}

NOTICE = (
    "Внутренний документ учебной системы Logiflow. Подтверждения учетными "
    "записями не являются УКЭП. Документ не заменяет транспортную накладную, "
    "путевой лист или подписанный сторонами первичный учетный документ."
)


def render_operational_document(event: DocumentRequested, font_path: str) -> bytes:
    pdfmetrics.registerFont(TTFont("LogiflowBody", font_path))
    output = BytesIO()
    accent = colors.HexColor("#243B63")
    base = {"fontName": "LogiflowBody", "fontSize": 10, "leading": 13.5}
    body = ParagraphStyle("body", **base, spaceAfter=5)
    heading = ParagraphStyle(
        "heading", **base, textColor=accent, spaceBefore=3, keepWithNext=True
    )
    title = ParagraphStyle(
        "title",
        fontName="LogiflowBody",
        fontSize=21,
        leading=27,
        textColor=accent,
        spaceAfter=20,
    )
    small = ParagraphStyle(
        "small",
        fontName="LogiflowBody",
        fontSize=8,
        leading=12,
        textColor=colors.HexColor("#536173"),
        spaceAfter=12,
    )
    story = [
        Paragraph(TITLES[event.type], title),
        Paragraph(escape(NOTICE), small),
        Paragraph(f"Заказ: {event.order_id}<br/>Основание: {event.reference_id}", body),
        Paragraph(
            f"Событие: {event.event_id}<br/>Время: {event.occurred_at.isoformat()}",
            small,
        ),
        HRFlowable(width="100%", color=accent),
        Spacer(1, 4 * mm),
    ]
    for field in event.fields:
        story.append(Paragraph(escape(field.label), heading))
        story.append(Paragraph(escape(field.value).replace("\n", "<br/>") or "-", body))

    def page(canvas, doc):
        canvas.saveState()
        canvas.setFillColor(accent)
        canvas.rect(0, 0, 5 * mm, A4[1], fill=1, stroke=0)
        canvas.setFont("LogiflowBody", 8)
        canvas.drawString(22 * mm, 14 * mm, "LOGIFLOW / Внутренний документ")
        canvas.drawRightString(A4[0] - 20 * mm, 14 * mm, f"Страница {doc.page}")
        canvas.restoreState()

    doc = SimpleDocTemplate(
        output,
        pagesize=A4,
        rightMargin=20 * mm,
        leftMargin=22 * mm,
        topMargin=20 * mm,
        bottomMargin=24 * mm,
        title=TITLES[event.type],
    )
    doc.build(story, onFirstPage=page, onLaterPages=page)
    return output.getvalue()


async def accept_operational_document(
    db: AsyncSession, event: DocumentRequested, font_path: str
) -> Document:
    existing = await db.scalar(
        select(Document).where(
            Document.reference_id == event.reference_id, Document.type == event.type
        )
    )
    if existing is not None:
        return existing
    document = Document(
        order_id=event.order_id,
        reference_id=event.reference_id,
        user_id=event.user_id,
        type=event.type,
        title=TITLES[event.type],
        pdf_bytes=render_operational_document(event, font_path),
    )
    db.add(document)
    await db.flush()
    add_ready_event(db, document, event.recipients)
    await db.commit()
    return document
