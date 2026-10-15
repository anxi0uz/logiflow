// seed adds an atomic, repeatable demonstration dataset to an already migrated DB.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/services"
)

func main() {
	// Storage error logs include SQL arguments; never log password hashes here.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Demo seed:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := config.NewConfig(ctx, "configs/config.toml")
	if err != nil {
		return err
	}
	pool, err := database.NewConnectionPool(ctx, cfg.DatabaseURL())
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	password := os.Getenv("LOGIFLOW_DEMO_PASSWORD")
	if password == "" {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		password = hex.EncodeToString(random[:])
	}
	created, err := services.NewOrderService(pool, *cfg).SeedDemo(ctx, password)
	if err != nil {
		return err
	}
	if !created {
		fmt.Println("Demo dataset already exists; accounts, passwords and progress unchanged.")
		return nil
	}
	fmt.Println("Created 12 users, 3 warehouses, 6 vehicles/drivers, 7 counterparties and 9 orders.")
	fmt.Println("Accounts: admin@demo.logiflow.test, manager.moscow@demo.logiflow.test,")
	fmt.Println("manager.spb@demo.logiflow.test, manager.kazan@demo.logiflow.test,")
	fmt.Println("client1@demo.logiflow.test, client2@demo.logiflow.test, driver1..6@demo.logiflow.test")
	fmt.Println("Password for the new demo accounts:", password)
	fmt.Println("PDF and Dispatch events are queued; the running services process them automatically.")
	return nil
}
