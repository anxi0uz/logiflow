package models

// OrderReferences contains display labels only, scoped to one accessible order.
// These are current catalog names, not immutable transport/document snapshots.
type OrderReferences struct {
	Users       map[string]string `json:"users"`
	Drivers     map[string]string `json:"drivers"`
	Vehicles    map[string]string `json:"vehicles"`
	Warehouses  map[string]string `json:"warehouses"`
	Assignments map[string]string `json:"assignments"`
}
