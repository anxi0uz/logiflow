package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestRefreshTokenIdentifiesUserAndRotates(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	redisAddr := os.Getenv("LOGIFLOW_TEST_REDIS_ADDR")
	if databaseURL == "" || redisAddr == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL and LOGIFLOW_TEST_REDIS_ADDR are not set")
	}

	ctx := context.Background()
	t.Chdir("../..")
	if err := database.RunMigrations(ctx, databaseURL); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = redisClient.Close() })

	user := models.User{
		ID:           uuid.New(),
		Email:        uuid.NewString() + "@refresh.test",
		Slug:         "refresh-" + uuid.NewString(),
		PasswordHash: "test",
		FullName:     "Refresh Test",
		Role:         "client",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := storage.Create(ctx, "users", user, pool); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.User](ctx, "users", pool, func(db *sqlbuilder.DeleteBuilder) {
			db.Where(db.EQ("id", user.ID))
		})
	})

	var cfg config.Config
	cfg.JwtOpt.Key = "integration-test-key"
	cfg.JwtOpt.Issuer = "logiflow-test"
	cfg.Redis.AccessTokenDur = time.Hour
	cfg.Redis.RefreshTokenDur = 24 * time.Hour
	server := &Server{DB: pool, Redis: redisClient, Config: &cfg, JwtKey: []byte(cfg.JwtOpt.Key)}

	issueRequest := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	issueResponse := httptest.NewRecorder()
	server.issueTokens(issueResponse, issueRequest, &user)
	if issueResponse.Code != http.StatusOK {
		t.Fatalf("issue tokens: status=%d body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	oldCookie := refreshCookie(t, issueResponse.Result().Cookies())
	oldKey := "refresh_token:" + oldCookie.Value
	storedUserID, err := redisClient.Get(ctx, oldKey).Result()
	if err != nil || storedUserID != user.ID.String() {
		t.Fatalf("refresh identity: value=%q err=%v", storedUserID, err)
	}

	refreshRequest := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshRequest.AddCookie(oldCookie)
	refreshResponse := httptest.NewRecorder()
	server.AuthRefresh(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh tokens: status=%d body=%s", refreshResponse.Code, refreshResponse.Body.String())
	}
	if _, err := redisClient.Get(ctx, oldKey).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("old refresh token was not revoked: %v", err)
	}
	newCookie := refreshCookie(t, refreshResponse.Result().Cookies())
	if newCookie.Value == oldCookie.Value {
		t.Fatal("refresh token was not rotated")
	}
	storedUserID, err = redisClient.Get(ctx, "refresh_token:"+newCookie.Value).Result()
	if err != nil || storedUserID != user.ID.String() {
		t.Fatalf("rotated refresh identity: value=%q err=%v", storedUserID, err)
	}
}

func refreshCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == "refresh_token" {
			return cookie
		}
	}
	t.Fatal("refresh cookie not found")
	return nil
}
