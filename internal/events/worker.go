package events

import (
	"context"
	"encoding/base64"
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

type invalidEventError struct{ reason string }

func (e invalidEventError) Error() string { return e.reason }

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
	if _, err := js.StreamInfo("LOGIFLOW_INVALID_EVENTS"); err != nil {
		if _, err := js.AddStream(&nats.StreamConfig{
			Name: "LOGIFLOW_INVALID_EVENTS", Subjects: []string{"invalid.events.v1"},
		}); err != nil {
			if _, infoErr := js.StreamInfo("LOGIFLOW_INVALID_EVENTS"); infoErr != nil {
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
			if err := handleDocumentReady(message.Data,
				func(data []byte) error { return receiveDocumentReady(ctx, db, data) },
				func(reason string) error { return isolateInvalidEvent(ctx, js, message, reason) },
			); err != nil {
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

func handleDocumentReady(data []byte, receive func([]byte) error, isolate func(string) error) error {
	err := receive(data)
	var invalid invalidEventError
	if errors.As(err, &invalid) {
		return isolate(invalid.reason)
	}
	return err
}

func isolateInvalidEvent(ctx context.Context, js nats.JetStreamContext, message *nats.Msg, reason string) error {
	metadata, err := message.Metadata()
	if err != nil {
		return err
	}
	diagnostic, err := json.Marshal(struct {
		SourceStream   string `json:"source_stream"`
		SourceSequence uint64 `json:"source_sequence"`
		SourceSubject  string `json:"source_subject"`
		Consumer       string `json:"consumer"`
		Reason         string `json:"reason"`
		PayloadBase64  string `json:"payload_base64"`
	}{metadata.Stream, metadata.Sequence.Stream, message.Subject, "core-document-ready", reason, base64.StdEncoding.EncodeToString(message.Data)})
	if err != nil {
		return err
	}
	id := fmt.Sprintf("invalid:%s:%d:core-document-ready", metadata.Stream, metadata.Sequence.Stream)
	if _, err := js.Publish("invalid.events.v1", diagnostic, nats.MsgId(id), nats.Context(ctx)); err != nil {
		return err
	}
	slog.ErrorContext(ctx, "invalid document event isolated", slog.String("stream", metadata.Stream), slog.Uint64("sequence", metadata.Sequence.Stream), slog.String("reason", reason))
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
		return invalidEventError{fmt.Sprintf("decode document ready: %v", err)}
	}
	if event.EventID == uuid.Nil || event.DocumentID == uuid.Nil || event.OrderID == uuid.Nil || event.UserID == uuid.Nil || event.Type != "delivery_confirmation" {
		return invalidEventError{"invalid document ready required IDs or type"}
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
