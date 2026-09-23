package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMarkNotificationReadMissingReturnsNotFound(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL is not set")
	}
	t.Chdir("../..")
	ctx := context.Background()
	if err := database.RunMigrations(ctx, databaseURL); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	request := httptest.NewRequest(http.MethodPatch, "/notifications/missing/read", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: uuid.New(), Role: "client"}))
	response := httptest.NewRecorder()
	(&Server{DB: pool}).MarkNotificationRead(response, request, uuid.New())
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing notification: status=%d body=%s", response.Code, response.Body.String())
	}
}
