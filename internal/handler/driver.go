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
	"github.com/gosimple/slug"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) ListDrivers(w http.ResponseWriter, r *http.Request, params api.ListDriversParams) {
	ctx := r.Context()

	drivers, err := storage.GetAll[models.Driver](ctx, "drivers", s.DB, func(sb *sqlbuilder.SelectBuilder) {
		if params.Status != nil {
			sb.Where(sb.Equal("status", string(*params.Status)))
		}
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting all drivers with that status")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, drivers, RespSuccess)
}

func (s *Server) CreateDriver(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "admin" {
		slog.WarnContext(ctx, "unusual try from not allowed role", slog.String("Role", claims.Role), slog.String("id", claims.ID.String()))
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}
	var req api.DriverCreate

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Invalid request body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.ErrorContext(ctx, "error while generating passwordhash", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	now := time.Now()
	userid := uuid.New()
	user := models.User{
		ID:           userid,
		Slug:         s.GenerateUserSlug(req.FullName, userid),
		CreatedAt:    now,
		UpdatedAt:    now,
		Role:         "driver",
		Email:        string(req.Email),
		PasswordHash: string(passwordHash),
		FullName:     req.FullName,
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Unable to open transaction", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.ErrorContext(ctx, "tx rollback failed", slog.String("error", err.Error()))
		}
	}()

	if err := storage.Create(ctx, "users", user, tx); err != nil {
		slog.ErrorContext(ctx, "Unable to create user", slog.String("error", err.Error()), slog.Any("user", user))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	id := uuid.New()
	driver := models.Driver{
		ID:            id,
		UserID:        userid,
		VehicleID:     req.VehicleId,
		LicenseNumber: req.LicenseNumber,
		LicenseExpiry: req.LicenseExpiry.Time,
		Rating:        0,
		Slug:          slug.Make(req.FullName + " " + req.LicenseNumber),
	}

	if err := storage.Create(ctx, "drivers", driver, tx); err != nil {
		slog.ErrorContext(ctx, "Unable to create driver", slog.String("error", err.Error()), slog.Any("driver", driver))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "Error while commiting transaction", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusCreated, map[string]any{
		"user":   user,
		"driver": driver,
	}, "driver")
}

func (s *Server) UpdateMyDriverStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok || claims == nil {
		slog.ErrorContext(ctx, "Unable to convert claims", slog.Any("claims", ctx.Value(UserKey)))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	var req api.DriverStatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	driver, err := storage.GetOne[models.Driver](ctx, s.DB, "drivers", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("user_id", claims.ID))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "No driver with that user id not found", slog.String("user id", claims.ID.String()))
			s.JSON(w, r, http.StatusNotFound, "driver not found", RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting driver for update", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	driver.Status = string(req.Status)
	if err := storage.Update(ctx, "drivers", driver, s.DB, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.Equal("user_id", claims.ID))
	}); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "No driver with that user id not found", slog.String("user id", claims.ID.String()))
			s.JSON(w, r, http.StatusNotFound, "driver not found", RespNotFound)
			return
		}
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	s.JSON(w, r, http.StatusOK, "status updated", RespSuccess)
}

func (s *Server) GetDriver(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	driver, err := storage.GetOne[models.Driver](ctx, s.DB, "drivers", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "No driver found with that slug", slog.String("slug", slug))
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while finding driver with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	s.JSON(w, r, http.StatusOK, driver, RespSuccess)
}

func (s *Server) UpdateDriver(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	var req api.DriverUpdate

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Invalid request body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInternalError, RespError)
		return
	}

	driver, err := storage.GetOne[models.Driver](ctx, s.DB, "drivers", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "No driver with that slug not found", slog.String("slug", slug))
			s.JSON(w, r, http.StatusNotFound, "driver not found", RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting driver for update", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	driver.LicenseNumber = *req.LicenseNumber
	driver.LicenseExpiry = req.LicenseExpiry.Time
	driver.Status = string(*req.Status)
	driver.VehicleID = req.VehicleId

	if err := storage.Update(ctx, "drivers", driver, s.DB, func(sb *sqlbuilder.UpdateBuilder) {
		sb.Where(sb.Equal("slug", slug))
	}); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "no drivers found with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while updating driver with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, slug, RespSuccess)
}

func (s *Server) DeleteDriver(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	err := storage.Delete[models.Driver](ctx, "drivers", s.DB, func(sb *sqlbuilder.DeleteBuilder) {
		sb.Where(sb.Equal("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.JSON(w, r, http.StatusNotFound, "No driver with that slug found", RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "unable to delete vehicle with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, slug, "deleted")

}
