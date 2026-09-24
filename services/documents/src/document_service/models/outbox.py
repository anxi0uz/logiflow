from datetime import datetime
from uuid import UUID, uuid4

from sqlalchemy import JSON, DateTime, String, Uuid, func
from sqlalchemy.orm import Mapped, mapped_column

from document_service.db.base import Base


class OutboxEvent(Base):
    __tablename__ = "document_outbox"

    id: Mapped[UUID] = mapped_column(Uuid, primary_key=True, default=uuid4)
    subject: Mapped[str] = mapped_column(String(128))
    payload: Mapped[dict] = mapped_column(JSON)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now()
    )
    published_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
