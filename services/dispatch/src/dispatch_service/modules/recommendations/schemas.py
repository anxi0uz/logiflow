from datetime import datetime
from typing import Self
from uuid import UUID

from pydantic import BaseModel, Field, model_validator


class DriverSnapshot(BaseModel):
    id: UUID
    rating: float = Field(ge=0, le=5)


class VehicleSnapshot(BaseModel):
    id: UUID
    capacity_kg: float = Field(ge=0)
    capacity_m3: float = Field(ge=0)


class RecommendationTrigger(BaseModel):
    event_id: UUID
    order_id: UUID
    submitted_at: datetime
    requested_at: datetime


class RecommendationRequest(BaseModel):
    event_id: UUID
    order_id: UUID
    submitted_at: datetime
    requested_at: datetime
    planned_from: datetime
    planned_to: datetime
    weight_kg: float = Field(ge=0)
    volume_m3: float = Field(ge=0)
    drivers: list[DriverSnapshot]
    vehicles: list[VehicleSnapshot]

    @model_validator(mode="after")
    def valid_window(self) -> Self:
        if self.planned_from >= self.planned_to:
            raise ValueError("planned_from must precede planned_to")
        return self


class RecommendationCandidate(BaseModel):
    id: str
    driver_id: UUID
    vehicle_id: UUID
    score: float
    reason: str


class RecommendationResult(BaseModel):
    event_id: UUID
    order_id: UUID
    submitted_at: datetime
    requested_at: datetime
    created_at: datetime
    candidates: list[RecommendationCandidate]
