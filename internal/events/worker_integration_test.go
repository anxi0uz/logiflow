package events

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDocumentReadyCreatesOneLinkedNotification(t *testing.T) {
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
	defer pool.Close()

	userID := uuid.New()
	if err := storage.Create(ctx, "users", models.User{
		ID: userID, Email: userID.String() + "@example.com", Slug: userID.String(),
		PasswordHash: "test", FullName: "Document owner", Role: "client", CreatedAt: time.Now(),
	}, pool); err != nil {
		t.Fatalf("create user: %v", err)
	}
	event := DocumentReady{
		EventID: uuid.New(), DocumentID: uuid.New(), OrderID: uuid.New(),
		UserID: userID, Type: "delivery_confirmation",
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := receiveDocumentReady(ctx, pool, data); err != nil {
			t.Fatalf("receive document ready: %v", err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND document_id = $2`, userID, event.DocumentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one linked notification, got %d", count)
	}
}
