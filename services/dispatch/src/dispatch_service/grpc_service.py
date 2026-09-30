import asyncpg
import grpc
from pydantic import ValidationError

from dispatch_service.modules.recommendations.schemas import RecommendationTrigger
from dispatch_service.modules.recommendations.service import RecommendationService
from logiflow.dispatch.v1 import dispatch_pb2, dispatch_pb2_grpc


class DispatchRecommendations(dispatch_pb2_grpc.DispatchRecommendationsServicer):
    def __init__(self, service: RecommendationService) -> None:
        self.service = service

    async def Recommend(
        self,
        request: dispatch_pb2.RecommendRequest,
        context: grpc.aio.ServicerContext,
    ) -> dispatch_pb2.RecommendResponse:
        try:
            trigger = RecommendationTrigger.model_validate_json(request.snapshot_json)
        except ValidationError as exc:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))
        try:
            result = await self.service.recommend(trigger)
        except (asyncpg.PostgresError, OSError, TimeoutError) as exc:
            await context.abort(grpc.StatusCode.UNAVAILABLE, str(exc))
        if result is None:
            await context.abort(
                grpc.StatusCode.FAILED_PRECONDITION, "order is no longer ready"
            )
        return dispatch_pb2.RecommendResponse(
            recommendations_json=result.model_dump_json().encode()
        )
