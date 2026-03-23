package handler

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) GetRoute(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) RouteWebSocket(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}
