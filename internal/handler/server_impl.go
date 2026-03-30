package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/xid"
	slogchi "github.com/samber/slog-chi"
)

type ctxKey string

const (
	requestIDKey ctxKey = "X-Request-ID"
	tokenKey     ctxKey = "Authorization"

	// response messages
	MsgInternalError = "Internal server error"
	MsgInvalidBody   = "Invalid request body"
	MsgNotFound      = "Not found"
	MsgUnauthorized  = "Unauthorized"
	MsgMissingToken  = "Missing token"
	MsgForbidden     = "Forbidden"

	// response types
	RespError    = "error"
	RespSuccess  = "success"
	RespNotFound = "not found"
)

type responseOptions struct {
	respType  string
	requestID string
}

type Server struct {
	DB          *pgxpool.Pool
	Config      *config.Config
	ctx         context.Context
	Redis       *redis.Client
	JwtKey      []byte
	OrderSerice services.OrderService
}

func NewServer(db *pgxpool.Pool, redis *redis.Client, cfg *config.Config) *Server {
	return &Server{
		DB:          db,
		Redis:       redis,
		ctx:         context.Background(),
		Config:      cfg,
		JwtKey:      []byte(cfg.JwtOpt.Key),
		OrderSerice: *services.NewOrderService(db, *cfg),
	}
}

func (s *Server) Run() error {
	r := chi.NewMux()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(s.MiddlewareRequestID)

	r.Use(slogchi.NewWithConfig(slog.Default(), slogchi.Config{
		DefaultLevel:     slog.LevelInfo,
		ClientErrorLevel: slog.LevelWarn,  // 400–499 → Warn
		ServerErrorLevel: slog.LevelError, // 500+   → Error
		WithRequestID:    true,            // берёт request-id из контекста
		Filters: []slogchi.Filter{
			slogchi.IgnorePath("/health", "/metrics", "/favicon.ico"),
		},
	}))

	r.Use(s.AuthMiddleware)

	h := api.HandlerFromMux(s, r)

	srv := &http.Server{
		Handler:      h,
		Addr:         s.Config.ServerURL(),
		ReadTimeout:  s.Config.ReadTimeout(),
		IdleTimeout:  s.Config.IdleTimeout(),
		WriteTimeout: s.Config.WriteTimeout(),
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP сервер упал", "error", err)
		}
	}()

	slog.Info("Приложение запущено успешно ", slog.String("URL", s.Config.ServerURL()))

	<-s.ctx.Done()

	slog.Info("Остановка HTTP сервера...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	return srv.Shutdown(shutdownCtx)
}

func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/register" || r.URL.Path == "/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		tokenStr := r.Header.Get("Authorization")
		if tokenStr == "" {
			s.JSON(w, r, http.StatusUnauthorized, MsgMissingToken, RespError)
			return
		}

		slog.Info("Проверка авторизации")

		claims, err := s.validateAccessToken(r.Context(), tokenStr)
		if err != nil {
			slog.WarnContext(r.Context(), "Токен не прошел валидацию", slog.String("Token", tokenStr), slog.String("error", err.Error()))
			s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
			return
		}

		ctx := context.WithValue(r.Context(), "user", claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) MiddlewareRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = xid.New().String()
		}

		ctx := context.WithValue(r.Context(), requestIDKey, rid)

		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) JSON(w http.ResponseWriter, r *http.Request, status int, payload any, respType string) {
	options := responseOptions{
		respType:  respType,
		requestID: extractRequestID(r),
	}

	success := status >= 200 && status < 300

	resp := api.ApiResponse{
		RequestID: &options.requestID,
		Status:    &status,
		Success:   &success,
		Data:      &map[string]interface{}{respType: payload},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.ErrorContext(r.Context(), "json encode failed after header written",
			slog.Int("status", status),
			slog.String("error", err.Error()),
			slog.String("request_id", options.requestID),
		)
	}
}

func extractRequestID(r *http.Request) string {
	if r == nil {
		return ""
	}

	if v := r.Context().Value(requestIDKey); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}

	return ""
}

func (s *Server) validateAccessToken(ctx context.Context, tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			slog.WarnContext(ctx, "неизвестный алгоритм", "alg", t.Header["alg"])
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.JwtKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("token parse error: %w", err)
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	if token.Header["alg"] != "HS256" {
		return nil, errors.New("only HS256 allowed")
	}

	redisKey := "access_token:" + tokenStr
	if _, err := s.Redis.Get(ctx, redisKey).Result(); err != nil {
		slog.ErrorContext(ctx, "redis error during token validation", "error", err.Error())
		return nil, fmt.Errorf("redis error: %w", err)
	}

	if claims.TokenID == "" {
		return nil, errors.New("missing token id (jti)")
	}

	if claims.ID == uuid.Nil {
		return nil, errors.New("missing user id in claims")
	}

	return claims, nil
}
