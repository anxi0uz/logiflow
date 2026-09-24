from datetime import datetime
from uuid import UUID

from pydantic import BaseModel, Field


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
    driver_id: UUID
    vehicle_id: UUID


class DocumentReady(BaseModel):
    event_id: UUID
    document_id: UUID
    order_id: UUID
    user_id: UUID
    type: str
