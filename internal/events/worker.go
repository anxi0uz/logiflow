package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

// Run relays committed Core events and receives document-ready notifications.
// Work remains in PostgreSQL or JetStream while this worker is down.
func Run(ctx context.Context, db *pgxpool.Pool, natsURL string) {
	for ctx.Err() == nil {
		if err := runConnected(ctx, db, natsURL); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "event worker disconnected", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func runConnected(ctx context.Context, db *pgxpool.Pool, natsURL string) error {
	nc, err := nats.Connect(natsURL, nats.Timeout(3*time.Second))
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	if _, err := js.StreamInfo("LOGIFLOW_EVENTS"); err != nil {
		if _, err := js.AddStream(&nats.StreamConfig{
			Name: "LOGIFLOW_EVENTS", Subjects: []string{"delivery.completed.v1", "document.ready.v1"},
		}); err != nil {
			// The document service can win the create race during startup.
			if _, infoErr := js.StreamInfo("LOGIFLOW_EVENTS"); infoErr != nil {
				return err
			}
		}
	}
	sub, err := js.PullSubscribe("document.ready.v1", "core-document-ready", nats.BindStream("LOGIFLOW_EVENTS"), nats.ManualAck())
	if err != nil {
		return err
	}

	for ctx.Err() == nil {
		if err := publishPending(ctx, db, js); err != nil {
			return err
		}
		messages, err := sub.Fetch(10, nats.MaxWait(time.Second))
		if err != nil && !errors.Is(err, nats.ErrTimeout) {
			return err
		}
		for _, message := range messages {
			if err := receiveDocumentReady(ctx, db, message.Data); err != nil {
				slog.ErrorContext(ctx, "document notification failed", slog.String("error", err.Error()))
				_ = message.NakWithDelay(5 * time.Second)
				continue
			}
			if err := message.Ack(); err != nil {
				return err
			}
		}
	}
	return nil
}

func publishPending(ctx context.Context, db *pgxpool.Pool, js nats.JetStreamContext) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `SELECT id, subject, payload FROM integration_outbox
		WHERE published_at IS NULL ORDER BY created_at, id LIMIT 20 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	type pending struct {
		id      uuid.UUID
		subject string
		payload []byte
	}
	var batch []pending
	for rows.Next() {
		var event pending
		if err := rows.Scan(&event.id, &event.subject, &event.payload); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, event := range batch {
		if _, err := js.Publish(event.subject, event.payload, nats.MsgId(event.id.String()), nats.Context(ctx)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE integration_outbox SET published_at = NOW() WHERE id = $1`, event.id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func receiveDocumentReady(ctx context.Context, db *pgxpool.Pool, data []byte) error {
	var event DocumentReady
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("decode document ready: %w", err)
	}
	if event.EventID == uuid.Nil || event.DocumentID == uuid.Nil || event.UserID == uuid.Nil {
		return fmt.Errorf("invalid document ready IDs")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	result, err := tx.Exec(ctx, `INSERT INTO integration_inbox(event_id) VALUES($1) ON CONFLICT DO NOTHING`, event.EventID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		var userExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, event.UserID).Scan(&userExists); err != nil {
			return err
		}
		if userExists {
			body := fmt.Sprintf("Подтверждение доставки заказа %s готово", event.OrderID)
			if err := storage.Create(ctx, "notifications", models.Notification{
				ID: uuid.New(), UserID: event.UserID, Title: "Документ готов",
				Body: &body, DocumentID: &event.DocumentID, CreatedAt: time.Now(),
			}, tx); err != nil {
				return err
			}
		} else {
			slog.WarnContext(ctx, "document owner no longer exists", slog.String("user_id", event.UserID.String()))
		}
	}
	return tx.Commit(ctx)
}
