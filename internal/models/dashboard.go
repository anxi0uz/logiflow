package models

import "github.com/google/uuid"

type DashboardRevenue struct {
	Total     float64 `json:"total"`
	ThisMonth float64 `json:"thisMonth"`
}

type DashboardOrderStatus struct {
	Total     int `json:"total"`
	Delivered int `json:"delivered"`
	InTransit int `json:"inTransit"`
	Pending   int `json:"pending"`
	Cancelled int `json:"cancelled"`
}

type DashboardDriverStat struct {
	ID              uuid.UUID `json:"id"`
	FullName        string    `json:"fullName"`
	Status          string    `json:"status"`
	Rating          float64   `json:"rating"`
	CompletedOrders int       `json:"completedOrders"`
}

type DashboardReport struct {
	Revenue DashboardRevenue      `json:"revenue"`
	Orders  DashboardOrderStatus  `json:"orders"`
	Drivers []DashboardDriverStat `json:"drivers"`
}
