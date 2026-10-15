package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestOrderReferencesAreReadableAndScoped(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 310)
	other := newExpiryFixture(t, pool, 312)
	if _, err := pool.Exec(ctx, `UPDATE users SET full_name='Михаил Соколов' WHERE id=$1`, f.driverUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE vehicles SET plate_number='А101ДМ77',brand='Ford',model='Transit' WHERE id=$1`, f.vehicle.ID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []struct {
		id   uuid.UUID
		role string
	}{
		{f.ownerID, "client"}, {f.driverUserID, "driver"}, {f.ownerID, "admin"},
	} {
		refs, err := f.service.GetOrderReferences(ctx, f.order.ID, actor.id, actor.role)
		if err != nil {
			t.Fatal(err)
		}
		if refs.Drivers[f.driver.ID.String()] != "Михаил Соколов" ||
			refs.Users[f.driverUserID.String()] != "Михаил Соколов" ||
			refs.Users[f.ownerID.String()] != "Expiry test" ||
			refs.Vehicles[f.vehicle.ID.String()] != "А101ДМ77 · Ford Transit" ||
			refs.Warehouses[f.warehouseID.String()] != "Expiry" ||
			refs.Assignments[f.offer.ID.String()] != "Михаил Соколов · А101ДМ77" {
			t.Fatalf("missing display labels: %+v", refs)
		}
		if len(refs.Users) != 2 || len(refs.Drivers) != 1 || len(refs.Vehicles) != 1 || len(refs.Warehouses) != 1 || len(refs.Assignments) != 1 {
			t.Fatalf("unrelated catalog entries leaked: %+v", refs)
		}
	}
	for _, actor := range []struct {
		id   uuid.UUID
		role string
	}{
		{other.ownerID, "client"}, {other.driverUserID, "driver"}, {f.ownerID, "manager"}, {f.ownerID, "unknown"},
	} {
		if _, err := f.service.GetOrderReferences(ctx, f.order.ID, actor.id, actor.role); !errors.Is(err, ErrForbidden) {
			t.Fatalf("unauthorized references (%s): %v", actor.role, err)
		}
	}
	f.makeExpired(t)
	if _, err := f.service.GetOrderReferences(ctx, f.order.ID, f.driverUserID, "driver"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expired offer retained labels access: %v", err)
	}
}
