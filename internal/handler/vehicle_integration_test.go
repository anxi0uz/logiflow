package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminCanManageGlobalFleet(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL is not set")
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
	server := &Server{DB: pool}
	adminID := uuid.New()
	plate := "AZ-" + uuid.NewString()[:8]
	request := httptest.NewRequest(http.MethodPost, "/vehicles", strings.NewReader(`{"plateNumber":"`+plate+`","capacityKg":1000}`))
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: adminID, Role: "admin"}))
	response := httptest.NewRecorder()
	server.CreateVehicle(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("admin create vehicle: status=%d body=%s", response.Code, response.Body.String())
	}
	vehicle, err := storage.GetOne[models.Vehicle](ctx, pool, "vehicles", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("plate_number", plate)) })
	if err != nil {
		t.Fatalf("created vehicle not found: %v", err)
	}
	if vehicle.Brand != nil || vehicle.Model != nil || vehicle.Year != nil || vehicle.CapacityKg != 1000 || vehicle.CapacityM3 != 0 {
		t.Fatalf("optional vehicle fields were not preserved: %+v", vehicle)
	}
	sparseID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO vehicles (id, plate_number, slug) VALUES ($1, $2, $3)`, sparseID, "SP-"+sparseID.String()[:8], "sparse-"+sparseID.String()); err != nil {
		t.Fatalf("insert sparse vehicle: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM vehicles WHERE id = $1`, sparseID)
	})
	sparse, err := storage.GetOne[models.Vehicle](ctx, pool, "vehicles", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", sparseID)) })
	if err != nil || sparse.Brand != nil || sparse.Model != nil || sparse.Year != nil || sparse.CapacityKg != 0 || sparse.CapacityM3 != 0 || sparse.Status != "maintenance" {
		t.Fatalf("scan sparse vehicle: %+v err=%v", sparse, err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.Vehicle](ctx, "vehicles", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", vehicle.ID)) })
	})

	validUntil := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	request = httptest.NewRequest(http.MethodPost, "/vehicles/"+vehicle.Slug+"/documents", strings.NewReader(`{"type":"registration","number":"AUTHZ-REG","validUntil":"`+validUntil+`"}`))
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: adminID, Role: "admin"}))
	response = httptest.NewRecorder()
	server.CreateVehicleDocument(response, request, vehicle.Slug)
	if response.Code != http.StatusCreated {
		t.Fatalf("admin create vehicle document: status=%d body=%s", response.Code, response.Body.String())
	}
	document, err := storage.GetOne[models.VehicleDocument](ctx, pool, "vehicle_documents", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("vehicle_id", vehicle.ID)) })
	if err != nil {
		t.Fatalf("created vehicle document not found: %v", err)
	}
	request = httptest.NewRequest(http.MethodPatch, "/vehicles/"+vehicle.Slug+"/documents/"+document.ID.String(), strings.NewReader(`{"status":"suspended"}`))
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: adminID, Role: "admin"}))
	response = httptest.NewRecorder()
	server.UpdateVehicleDocument(response, request, vehicle.Slug, document.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("admin update vehicle document: status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodDelete, "/vehicles/"+vehicle.Slug, nil)
	request = request.WithContext(context.WithValue(request.Context(), UserKey, &Claims{ID: adminID, Role: "admin"}))
	response = httptest.NewRecorder()
	server.DeleteVehicle(response, request, vehicle.Slug)
	if response.Code != http.StatusOK {
		t.Fatalf("admin delete vehicle: status=%d body=%s", response.Code, response.Body.String())
	}
}
