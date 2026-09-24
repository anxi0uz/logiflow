package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/documentpb"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type inboxClientStub struct {
	request *documentpb.ListDocumentsRequest
	empty   bool
}

func (s *inboxClientStub) ListDocuments(_ context.Context, request *documentpb.ListDocumentsRequest, _ ...grpc.CallOption) (*documentpb.ListDocumentsResponse, error) {
	s.request = request
	if s.empty {
		return &documentpb.ListDocumentsResponse{}, nil
	}
	return &documentpb.ListDocumentsResponse{Documents: []*documentpb.Document{{Id: uuid.NewString()}}}, nil
}

func (s *inboxClientStub) DownloadDocument(context.Context, *documentpb.DownloadDocumentRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[documentpb.DocumentChunk], error) {
	return nil, nil
}

func TestGeneratedDocumentInboxRoute(t *testing.T) {
	client := &inboxClientStub{}
	server := &Server{DocumentClient: client}
	ownerID := uuid.New()
	router := api.HandlerFromMux(server, chi.NewMux())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/documents?limit=5&offset=2", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: ownerID}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || client.request.GetUserId() != ownerID.String() || client.request.GetLimit() != 5 || client.request.GetOffset() != 2 {
		t.Fatalf("inbox request: status=%d grpc=%+v", response.Code, client.request)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/documents?limit=101", nil)
	invalid = invalid.WithContext(context.WithValue(invalid.Context(), UserKey, &Claims{ID: ownerID}))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination status: %d", response.Code)
	}

	client.empty = true
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":[]`) {
		t.Fatalf("empty inbox response: %d %s", response.Code, response.Body.String())
	}
}
