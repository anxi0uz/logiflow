from uuid import UUID

import grpc
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from document_service.models import Document
from logiflow.documents.v1 import documents_pb2, documents_pb2_grpc


class DocumentInbox(documents_pb2_grpc.DocumentInboxServicer):
    def __init__(self, sessions: async_sessionmaker[AsyncSession]) -> None:
        self.sessions = sessions

    async def ListDocuments(self, request, context):
        try:
            user_id = UUID(request.user_id)
        except ValueError:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "invalid user_id")

        limit = min(max(request.limit or 20, 1), 100)
        offset = min(request.offset, 10_000)
        async with self.sessions() as db:
            rows = await db.scalars(
                select(Document)
                .where(Document.user_id == user_id)
                .order_by(Document.created_at.desc(), Document.id.desc())
                .offset(offset)
                .limit(limit)
            )
            return documents_pb2.ListDocumentsResponse(
                documents=[
                    documents_pb2.Document(
                        id=str(item.id),
                        order_id=str(item.order_id),
                        type=item.type,
                        title=item.title,
                        created_at=item.created_at.isoformat(),
                        size_bytes=len(item.pdf_bytes),
                    )
                    for item in rows
                ]
            )

    async def DownloadDocument(self, request, context):
        try:
            user_id = UUID(request.user_id)
            document_id = UUID(request.document_id)
        except ValueError:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "invalid ID")

        async with self.sessions() as db:
            document = await db.get(Document, document_id)
            if document is None or document.user_id != user_id:
                await context.abort(grpc.StatusCode.NOT_FOUND, "document not found")
            for offset in range(0, len(document.pdf_bytes), 64 * 1024):
                yield documents_pb2.DocumentChunk(
                    data=document.pdf_bytes[offset : offset + 64 * 1024]
                )
