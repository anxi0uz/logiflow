from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, Field, field_validator


class DocumentField(BaseModel):
    label: str = Field(min_length=1, max_length=200)
    value: str = Field(max_length=20000)


class DeliveryCompleted(BaseModel):
    event_id: UUID
    order_id: UUID
    user_id: UUID
    origin_address: str
    destination_address: str
    cargo_description: str
    weight_kg: float = Field(ge=0)
    volume_m3: float = Field(ge=0)
    total_price: float | None = Field(default=None, ge=0)
    delivered_at: datetime
    recipient_name: str | None = None
    fields: list[DocumentField] = Field(default_factory=list, max_length=200)
    driver_id: UUID
    vehicle_id: UUID

    @field_validator("event_id", "order_id", "user_id", "driver_id", "vehicle_id")
    @classmethod
    def nonzero_id(cls, value: UUID) -> UUID:
        if value.int == 0:
            raise ValueError("ID must not be nil")
        return value


class DocumentReady(BaseModel):
    event_id: UUID
    document_id: UUID
    order_id: UUID
    user_id: UUID
    type: str


class DocumentRequested(BaseModel):
    event_id: UUID
    order_id: UUID
    user_id: UUID
    reference_id: UUID
    type: Literal[
        "transport_order",
        "trip_assignment",
        "cargo_handover",
        "service_report",
        "cancellation_notice",
    ]
    occurred_at: datetime
    recipients: list[UUID] = Field(default_factory=list, max_length=20)
    fields: list[DocumentField] = Field(min_length=1, max_length=200)

    @field_validator("event_id", "order_id", "user_id", "reference_id")
    @classmethod
    def nonzero_id(cls, value: UUID) -> UUID:
        if value.int == 0:
            raise ValueError("ID must not be nil")
        return value

    @field_validator("recipients")
    @classmethod
    def nonzero_recipients(cls, values: list[UUID]) -> list[UUID]:
        if any(value.int == 0 for value in values):
            raise ValueError("recipient ID must not be nil")
        return values

    @field_validator("occurred_at")
    @classmethod
    def aware_time(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("timestamp must have a timezone")
        return value
