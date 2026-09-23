package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestDeletingManagerDisablesAccountAndOldTokens(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	redisAddr := os.Getenv("LOGIFLOW_TEST_REDIS_ADDR")
	if databaseURL == "" || redisAddr == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL and LOGIFLOW_TEST_REDIS_ADDR are not set")
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
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = redisClient.Close() })

	warehouse := models.Warehouse{ID: uuid.New(), Name: "Auth warehouse", Slug: "auth-" + uuid.NewString(), Address: "Test", City: "Test", CreatedAt: time.Now()}
	if err := storage.Create(ctx, "warehouses", warehouse, pool); err != nil {
		t.Fatalf("create warehouse: %v", err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.Warehouse](ctx, "warehouses", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", warehouse.ID)) })
	})
	user := models.User{ID: uuid.New(), Email: uuid.NewString() + "@manager.test", Slug: "manager-" + uuid.NewString(), PasswordHash: "test", FullName: "Manager", Role: "manager", CreatedAt: time.Now()}
	if err := storage.Create(ctx, "users", user, pool); err != nil {
		t.Fatalf("create manager account: %v", err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.User](ctx, "users", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", user.ID)) })
	})
	manager := models.Manager{ID: uuid.New(), UserID: user.ID, WarehouseID: &warehouse.ID, Slug: "manager-" + uuid.NewString()}
	if err := storage.Create(ctx, "managers", manager, pool); err != nil {
		t.Fatalf("create manager profile: %v", err)
	}

	var cfg config.Config
	cfg.JwtOpt.Key = "integration-test-key"
	cfg.Redis.AccessTokenDur = time.Hour
	cfg.Redis.RefreshTokenDur = time.Hour
	server := &Server{DB: pool, Redis: redisClient, Config: &cfg, JwtKey: []byte(cfg.JwtOpt.Key)}
	access, err := server.generateAccessToken(&user, time.Hour)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}
	refresh := uuid.NewString()
	if err := redisClient.Set(ctx, "access_token:"+access, "valid", time.Hour).Err(); err != nil {
		t.Fatalf("store access token: %v", err)
	}
	if err := redisClient.Set(ctx, "refresh_token:"+refresh, user.ID.String(), time.Hour).Err(); err != nil {
		t.Fatalf("store refresh token: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Del(ctx, "access_token:"+access, "refresh_token:"+refresh).Err() })
	if _, err := server.validateAccessToken(ctx, access); err != nil {
		t.Fatalf("valid manager token rejected: %v", err)
	}

	user.Role = "client"
	if err := storage.Update(ctx, "users", user, pool, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", user.ID)) }); err != nil {
		t.Fatalf("change role: %v", err)
	}
	if _, err := server.validateAccessToken(ctx, access); err == nil {
		t.Fatal("token with stale manager role accepted")
	}
	user.Role = "manager"
	if err := storage.Update(ctx, "users", user, pool, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", user.ID)) }); err != nil {
		t.Fatalf("restore role: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/managers/"+manager.Slug, nil)
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: uuid.New(), Role: "admin"}))
	response := httptest.NewRecorder()
	server.DeleteManager(response, request, manager.Slug)
	if response.Code != http.StatusOK {
		t.Fatalf("delete manager: status=%d body=%s", response.Code, response.Body.String())
	}
	storedUser, err := storage.GetOne[models.User](ctx, pool, "users", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", user.ID)) })
	if err != nil || storedUser.Role != roleDisabled {
		t.Fatalf("manager account not disabled: %+v err=%v", storedUser, err)
	}
	if _, err := storage.GetOne[models.Manager](ctx, pool, "managers", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", manager.ID)) }); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("manager profile still exists: %v", err)
	}
	if _, err := server.validateAccessToken(ctx, access); err == nil {
		t.Fatal("deleted manager access token still works")
	}
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"`+user.Email+`","password":"test"}`))
	loginResponse := httptest.NewRecorder()
	server.AuthLogin(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("disabled manager login: status=%d", loginResponse.Code)
	}
	refreshRequest := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshRequest.AddCookie(&http.Cookie{Name: "refresh_token", Value: refresh})
	refreshResponse := httptest.NewRecorder()
	server.AuthRefresh(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusUnauthorized {
		t.Fatalf("disabled manager refresh: status=%d", refreshResponse.Code)
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
