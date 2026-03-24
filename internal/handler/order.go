package handler

import (
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) ListOrders(w http.ResponseWriter, r *http.Request, params api.ListOrdersParams) {}

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {}

func (s *Server) GetOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) UpdateOrderStatus(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) GetOrdersReport(w http.ResponseWriter, r *http.Request, params api.GetOrdersReportParams) {
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {}
