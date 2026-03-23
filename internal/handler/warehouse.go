package handler

import "net/http"

func (s *Server) ListWarehouses(w http.ResponseWriter, r *http.Request) {}

func (s *Server) CreateWarehouse(w http.ResponseWriter, r *http.Request) {}

func (s *Server) GetWarehouse(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) UpdateWarehouse(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) DeleteWarehouse(w http.ResponseWriter, r *http.Request, slug string) {}
