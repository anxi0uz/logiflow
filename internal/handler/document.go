package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/documentpb"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ListInboxDocuments(w http.ResponseWriter, r *http.Request, params api.ListInboxDocumentsParams) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	if s.DocumentClient == nil {
		s.JSON(w, r, http.StatusServiceUnavailable, "documents unavailable", RespError)
		return
	}
	limit, offset := 20, 0
	if params.Limit != nil {
		limit = *params.Limit
		if limit < 1 || limit > 100 {
			s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
			return
		}
	}
	if params.Offset != nil {
		offset = *params.Offset
		if offset < 0 || offset > 10_000 {
			s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := s.DocumentClient.ListDocuments(ctx, &documentpb.ListDocumentsRequest{
		UserId: claims.ID.String(), Limit: uint32(limit), Offset: uint32(offset),
	})
	if err != nil {
		slog.ErrorContext(ctx, "list documents failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusServiceUnavailable, "documents unavailable", RespError)
		return
	}
	documents := result.GetDocuments()
	if documents == nil {
		documents = []*documentpb.Document{}
	}
	s.JSON(w, r, http.StatusOK, documents, RespSuccess)
}

func (s *Server) DownloadInboxDocument(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	if s.DocumentClient == nil {
		s.JSON(w, r, http.StatusServiceUnavailable, "documents unavailable", RespError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stream, err := s.DocumentClient.DownloadDocument(ctx, &documentpb.DownloadDocumentRequest{
		UserId: claims.ID.String(), DocumentId: id.String(),
	})
	if err != nil {
		s.JSON(w, r, http.StatusServiceUnavailable, "documents unavailable", RespError)
		return
	}
	first, err := stream.Recv()
	if err != nil {
		if status.Code(err) == codes.NotFound {
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		s.JSON(w, r, http.StatusServiceUnavailable, "documents unavailable", RespError)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="delivery-confirmation.pdf"`)
	w.Header().Set("Cache-Control", "private, no-store")
	if _, err := w.Write(first.GetData()); err != nil {
		return
	}
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			slog.ErrorContext(ctx, "download document interrupted", slog.String("error", err.Error()))
			return
		}
		if _, err := w.Write(chunk.GetData()); err != nil {
			return
		}
	}
}
