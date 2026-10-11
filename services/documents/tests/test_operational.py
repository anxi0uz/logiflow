import asyncio
from datetime import UTC, datetime
from io import BytesIO
from uuid import UUID, uuid4

import pytest
from pydantic import ValidationError
from pypdf import PdfReader

from document_service.models import Document, DocumentRecipient, OutboxEvent
from document_service.modules.documents.operational import (
    TITLES,
    accept_operational_document,
    render_operational_document,
)
from document_service.modules.documents.schemas import DocumentRequested


def event(kind="cargo_handover", **updates):
    return DocumentRequested.model_validate(
        {
            "event_id": uuid4(),
            "order_id": uuid4(),
            "user_id": uuid4(),
            "reference_id": uuid4(),
            "type": kind,
            "occurred_at": datetime.now(UTC),
            "fields": [
                {"label": "Состояние груза", "value": "Упаковка целая <без замечаний>"}
            ],
            **updates,
        }
    )


@pytest.mark.parametrize("kind", TITLES)
def test_all_operational_documents_render_cyrillic_and_identifiers(font_path, kind):
    request = event(kind)
    raw = render_operational_document(request, font_path)
    pages = PdfReader(BytesIO(raw)).pages
    text = "\n".join(page.extract_text() for page in pages)
    assert TITLES[kind] in text
    assert str(request.reference_id) in text
    assert "Упаковка целая <без замечаний>" in text
    assert "не заменяет" in text


def test_long_field_continues_on_following_pages(font_path):
    value = "Сведения о грузе и передаче. " * 450
    request = event(
        fields=[
            {"label": "Подробности", "value": value},
            {"label": "Конец", "value": "Последняя строка"},
        ]
    )
    pages = PdfReader(BytesIO(render_operational_document(request, font_path))).pages
    assert len(pages) > 1
    assert "Последняя строка" in pages[-1].extract_text()


@pytest.mark.parametrize(
    "updates",
    [
        {"type": "signed_waybill"},
        {"reference_id": UUID(int=0)},
        {"occurred_at": "2026-10-11T00:00:00"},
        {"recipients": [UUID(int=0)]},
    ],
)
def test_invalid_requests_are_rejected(updates):
    with pytest.raises(ValidationError):
        event(**updates)


def test_duplicate_keeps_original_pdf_and_deduplicates_recipients(monkeypatch):
    from document_service.modules.documents import operational

    async def scenario():
        owner, driver = uuid4(), uuid4()
        request = event(user_id=owner, recipients=[owner, driver, driver])
        monkeypatch.setattr(
            operational, "render_operational_document", lambda *_: b"%PDF-original"
        )

        class Database:
            def __init__(self):
                self.rows = []
                self.document = None

            async def scalar(self, _query):
                return self.document

            def add(self, value):
                self.rows.append(value)

            async def flush(self):
                for item in self.rows:
                    if isinstance(item, Document):
                        item.id = uuid4()

            async def commit(self):
                self.document = next(
                    item for item in self.rows if isinstance(item, Document)
                )

        db = Database()
        first = await accept_operational_document(db, request, "unused")
        monkeypatch.setattr(
            operational,
            "render_operational_document",
            lambda *_: pytest.fail("duplicate rerendered"),
        )
        again = await accept_operational_document(db, request, "unused")
        assert again is first and again.pdf_bytes == b"%PDF-original"
        assert len([row for row in db.rows if isinstance(row, DocumentRecipient)]) == 1
        notices = [row for row in db.rows if isinstance(row, OutboxEvent)]
        assert len(notices) == 2
        assert {row.payload["user_id"] for row in notices} == {str(owner), str(driver)}

    asyncio.run(scenario())
