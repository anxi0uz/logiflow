package handler

import (
	"context"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/config"
)

func TestServerRunStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var cfg config.Config
	cfg.Server.URL = "127.0.0.1:0"

	server := NewServer(ctx, nil, nil, &cfg)
	result := make(chan error, 1)
	go func() { result <- server.Run() }()

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}
