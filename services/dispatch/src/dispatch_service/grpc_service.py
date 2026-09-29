import grpc
from pydantic import ValidationError

from dispatch_service.ranking import recommend
from dispatch_service.schemas import Request
from logiflow.dispatch.v1 import dispatch_pb2, dispatch_pb2_grpc


class DispatchRecommendations(dispatch_pb2_grpc.DispatchRecommendationsServicer):
    async def Recommend(self, request, context):
        try:
            snapshot = Request.model_validate_json(request.snapshot_json)
        except ValidationError as exc:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))
        result = recommend(snapshot)
        return dispatch_pb2.RecommendResponse(
            recommendations_json=result.model_dump_json().encode()
        )
