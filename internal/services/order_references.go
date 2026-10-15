package services

import (
	"context"

	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/google/uuid"
)

func (s *OrderService) GetOrderReferences(ctx context.Context, orderID, userID uuid.UUID, role string) (*models.OrderReferences, error) {
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	refs := &models.OrderReferences{
		Users: map[string]string{}, Drivers: map[string]string{}, Vehicles: map[string]string{},
		Warehouses: map[string]string{}, Assignments: map[string]string{},
	}
	// Resolve the order's participants in a batch. Never expose the global user directory.
	rows, err := s.db.Query(ctx, `WITH
o AS (SELECT * FROM orders WHERE id=$1),
a AS (SELECT * FROM assignments WHERE order_id=$1),
h AS (SELECT * FROM trip_handovers WHERE order_id=$1),
w AS (SELECT * FROM warehouse_operations WHERE order_id=$1),
i AS (SELECT * FROM order_incidents WHERE order_id=$1),
d AS (SELECT * FROM drivers WHERE id IN (
    SELECT driver_id FROM a UNION SELECT driver_id FROM h UNION SELECT driver_id FROM o)),
v AS (SELECT * FROM vehicles WHERE id IN (SELECT vehicle_id FROM a UNION SELECT vehicle_id FROM h)),
u AS (SELECT id,full_name FROM users WHERE id IN (
    SELECT user_id FROM d UNION SELECT created_by_id FROM o
    UNION SELECT created_by_user_id FROM a UNION SELECT created_by_user_id FROM h
    UNION SELECT created_by_user_id FROM w UNION SELECT reported_by_user_id FROM i
    UNION SELECT resolved_by_user_id FROM i
    UNION SELECT recorded_by_user_id FROM delivery_receipts WHERE order_id=$1
    UNION SELECT NULLIF(transport_details->>'recordedByUserId','')::uuid FROM o)),
wh AS (SELECT id,name FROM warehouses WHERE id IN (
    SELECT origin_warehouse_id FROM o UNION SELECT destination_warehouse_id FROM o
    UNION SELECT warehouse_id FROM w UNION SELECT warehouse_id FROM i))
SELECT 'users',id,full_name FROM u
UNION ALL SELECT 'drivers',d.id,u.full_name FROM d JOIN u ON u.id=d.user_id
UNION ALL SELECT 'vehicles',id,concat_ws(' · ',plate_number,NULLIF(concat_ws(' ',brand,model),'')) FROM v
UNION ALL SELECT 'warehouses',id,name FROM wh
UNION ALL SELECT 'assignments',a.id,concat_ws(' · ',u.full_name,v.plate_number)
    FROM a JOIN d ON d.id=a.driver_id JOIN u ON u.id=d.user_id JOIN v ON v.id=a.vehicle_id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := map[string]map[string]string{
		"users": refs.Users, "drivers": refs.Drivers, "vehicles": refs.Vehicles,
		"warehouses": refs.Warehouses, "assignments": refs.Assignments,
	}
	for rows.Next() {
		var kind, label string
		var id uuid.UUID
		if err := rows.Scan(&kind, &id, &label); err != nil {
			return nil, err
		}
		groups[kind][id.String()] = label
	}
	return refs, rows.Err()
}
