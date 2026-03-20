package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/huandu/go-sqlbuilder"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	ID      uuid.UUID `json:"id"`
	Email   string    `json:"email"`
	Role    string    `json:"role"`
	TokenID string    `json:"token_id"`
	jwt.RegisteredClaims
}

func (s *Server) AuthLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req api.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, "Ошибка при получении данных", "error")
		return
	}

	user, err := storage.GetOne[models.User](ctx, s.DB, "users", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("email", req.Email))
	})
	if err != nil {
		slog.WarnContext(ctx, "user with that email not found in db", slog.String("Email", string(req.Email)), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, "Пользователь с таким Email не найден", "error")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		slog.WarnContext(ctx, "Failed login to account with", slog.String("email:", string(req.Email)), slog.String("password from request", req.Password))
		s.JSON(w, r, http.StatusBadRequest, "Неверный пароль", "error")
		return
	}
	now := time.Now()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	err = storage.Update(ctx, "users", user, s.DB, func(sb *sqlbuilder.UpdateBuilder) {
		sb.Where(sb.Equal("id", user.ID))
	})
	s.issueTokens(w, r, user)
}

func (s *Server) AuthLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		s.JSON(w, r, http.StatusUnauthorized, "Missing refresh token", "error")
		return
	}
	refreshStr := cookie.Value
	if refreshStr == "" {
		s.JSON(w, r, http.StatusUnauthorized, "Empty refresh token", "error")
		return
	}

	refreshKey := "refresh_token:" + refreshStr
	if err := s.Redis.Del(ctx, refreshKey).Err(); err != nil {
		slog.ErrorContext(ctx, "Error while removing refresh token from redis", slog.String("token", refreshStr), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusUnauthorized, "Internal server error", "error")
		return
	}

	tokenStr := r.Header.Get("Authorization")
	if tokenStr == "" {
		s.JSON(w, r, http.StatusUnauthorized, "missing token", "error")
		return
	}

	tokenKey := "access_token:" + tokenStr
	if err := s.Redis.Del(ctx, tokenKey).Err(); err != nil {
		slog.ErrorContext(ctx, "Error while removing access token from redis", slog.String("token", tokenStr), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusUnauthorized, "Internal server error", "error")
		return
	}
}
func (s *Server) AuthRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		s.JSON(w, r, http.StatusUnauthorized, "Missing refresh token", "error")
		return
	}
	refreshStr := cookie.Value
	if refreshStr == "" {
		s.JSON(w, r, http.StatusUnauthorized, "Empty refresh token", "error")
		return
	}
	key := "refresh_token:" + refreshStr
	if _, err := s.Redis.Get(ctx, key).Result(); err == redis.Nil {
		s.JSON(w, r, http.StatusUnauthorized, "No active refresh token", "error")
		return
	}

	claimsValue := ctx.Value("user")

	claims, ok := claimsValue.(*Claims)

	if !ok {
		slog.ErrorContext(ctx, "Error parsing claims", slog.Any("claims", claims))
		s.JSON(w, r, http.StatusInternalServerError, nil, "internal server error")
		return
	}

	userID := claims.ID
	if userID == uuid.Nil {
		s.JSON(w, r, http.StatusUnauthorized, "Missing user id in token", "error")
		return
	}

	user, err := storage.GetOne[models.User](ctx, s.DB, "users", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("id", userID))
	})
	if err != nil {
		slog.ErrorContext(ctx, "user with that id not found", slog.Any("id", userID.String()), "error", err.Error())
		s.JSON(w, r, http.StatusUnauthorized, "Invalid user id in token", "error")
		return
	}
	s.issueTokens(w, r, user)
}
func (s *Server) AuthRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req api.RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, "invalid request body", "error")
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.JSON(w, r, http.StatusInternalServerError, "error while hashing password", "error")
		return
	}

	now := time.Now()

	uuid := uuid.New()

	user := models.User{
		ID:           uuid,
		Slug:         s.GenerateUserSlug(req.FullName, uuid),
		CreatedAt:    now,
		UpdatedAt:    now,
		Role:         string(req.Role),
		Email:        string(req.Email),
		PasswordHash: string(passwordHash),
		FullName:     req.FullName,
	}

	if err := storage.Create(ctx, "users", user, s.DB); err != nil {
		slog.ErrorContext(ctx, "Error while creating user", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "Server error", "error")
		return
	}
	s.issueTokens(w, r, &user)
}
func (s *Server) DeleteMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claimsValue := ctx.Value("user")
	claims, ok := claimsValue.(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while converting claims", slog.Any("claims", claimsValue))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}
	userID := claims.ID
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Error while begining transaction", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}
	defer tx.Rollback(ctx)

	err = storage.Delete[models.User](ctx, "users", tx, func(sb *sqlbuilder.DeleteBuilder) {
		sb.Where(sb.Equal("id", userID))
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error while deleting user", slog.String("error", err.Error()), slog.String("id", userID.String()))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}

	jwt := r.Header.Get("Authorization")
	tokenKey := "access_token" + jwt
	if err := s.Redis.Del(ctx, tokenKey).Err(); err != nil {
		slog.ErrorContext(ctx, "Error while deleting access token from redis", slog.String("token", jwt))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		s.JSON(w, r, http.StatusUnauthorized, "Missing refresh token", "error")
		return
	}
	refreshStr := cookie.Value
	if refreshStr == "" {
		s.JSON(w, r, http.StatusUnauthorized, "Empty refresh token", "error")
		return
	}
	key := "refresh_token:" + refreshStr
	if err := s.Redis.Del(ctx, key).Err(); err != nil {
		slog.ErrorContext(ctx, "Error while deleting refresh token from redis", slog.String("token", refreshStr))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "Error while committing transaction", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}

	s.JSON(w, r, http.StatusOK, "deleted", "ok")
}

func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claimsValue := ctx.Value("user")
	claims, ok := claimsValue.(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while converting claims", slog.Any("claims", claimsValue))
		s.JSON(w, r, http.StatusInternalServerError, "Internal server error", "error")
		return
	}

	userID := claims.ID
	user, err := storage.GetOne[models.User](ctx, s.DB, "users", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("id", userID))
	})
	if err != nil {
		slog.ErrorContext(ctx, "No user was found with that id",
			slog.String("id", userID.String()),
			slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "invalid user id", "error")
		return
	}
	s.JSON(w, r, http.StatusOK, user, "ok")
}
func (s *Server) UpdateMe(w http.ResponseWriter, r *http.Request) {}

func (s *Server) issueTokens(w http.ResponseWriter, r *http.Request, user *models.User) {
	access, err := s.generateAccessToken(user, s.Config.RedisAccessTokenDur())
	if err != nil {
		slog.ErrorContext(r.Context(), "generate access failed", slog.String("error", err.Error()))
		return
	}
	refresh, err := s.generateRefreshToken()
	if err != nil {
		slog.ErrorContext(r.Context(), "generate refresh token failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "Failure during generating tokens", "error")
		return
	}

	key := "access_token:" + access
	refreshkey := "refresh_token:" + refresh
	err = s.Redis.Set(r.Context(), key, "valid", s.Config.RedisAccessTokenDur()).Err()
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to set access token in redis", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "Server error", "error")
		return
	}

	err = s.Redis.Set(r.Context(), refreshkey, "valid", s.Config.RedisRefreshTokenDur()).Err()
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to set refresh token in redis", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, "Server error", "error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refresh,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   7 * 24 * 3600,
	})

	s.JSON(w, r, http.StatusOK, map[string]any{
		"access_token": access,
		"expires_in":   86400,
		"user":         user,
	}, "auth")
}

func (s *Server) deleteRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   "refresh_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

func (s *Server) generateAccessToken(user *models.User, duration time.Duration) (string, error) {
	return s.generateJWT(user, duration)
}

func (s *Server) generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) generateJWT(user *models.User, lifetime time.Duration) (string, error) {
	if len(s.JwtKey) == 0 {
		return "", errors.New("jwt key not set")
	}

	tokenID := hex.EncodeToString([]byte(time.Now().String() + user.ID.String()))

	claims := Claims{
		ID:      user.ID,
		Email:   user.Email,
		Role:    user.Role,
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(lifetime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   user.ID.String(),
			Issuer:    s.Config.JwtOpt.Issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.JwtKey)
}

func (s *Server) GenerateUserSlug(username string, uuid uuid.UUID) string {
	if username == "" {
		username = "user"
	}

	base := slug.Make(username)

	return base + "-" + uuid.String()
}
