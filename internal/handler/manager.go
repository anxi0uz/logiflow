package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) ListManagers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	managers, err := storage.GetAll[models.Manager](ctx, "managers", s.DB)
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting managers", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, managers, RespSuccess)
}

func (s *Server) CreateManager(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req api.ManagerCreate

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Error while decoding body", slog.Any("request", req), slog.String("Error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.ErrorContext(ctx, "error while generating passwordhash", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	userid := uuid.New()
	now := time.Now()

	userModel := models.User{
		ID:           userid,
		Slug:         s.GenerateUserSlug(req.FullName, userid),
		CreatedAt:    now,
		UpdatedAt:    now,
		Role:         "manager",
		Email:        string(req.Email),
		PasswordHash: string(passwordHash),
		FullName:     req.FullName,
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Error while opening transaction", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	defer tx.Rollback(ctx)

	if err := storage.Create(ctx, "users", userModel, tx); err != nil {
		slog.ErrorContext(ctx, "Error while creating user", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	managerid := uuid.New()
	managerModel := models.Manager{
		ID:          managerid,
		WarehouseID: req.WarehouseId,
		UserID:      userid,
		Slug:        s.GenerateUserSlug(req.FullName, managerid),
	}
	if err := storage.Create(ctx, "managers", managerModel, tx); err != nil {
		slog.ErrorContext(ctx, "Error while creating manager", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "Error while commiting transaction", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusCreated, map[string]any{
		"user":    userModel,
		"manager": managerModel,
	}, RespSuccess)
}

func (s *Server) GetManager(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	manager, err := storage.GetOne[models.Manager](ctx, s.DB, "managers", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "No manager was found with that slug", slog.String("slug", slug))
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting manager with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, manager, RespSuccess)
}

func (s *Server) DeleteManager(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	err := storage.Delete[models.Manager](ctx, "managers", s.DB, func(db *sqlbuilder.DeleteBuilder) {
		db.Where(db.EQ("slug", slug))
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error while deleting manager with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, "Deleted", RespSuccess)
}
