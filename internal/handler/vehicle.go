package handler

import "net/http"

func (s *Server) ListVehicles(w http.ResponseWriter, r *http.Request) {}

func (s *Server) CreateVehicle(w http.ResponseWriter, r *http.Request) {}

func (s *Server) GetVehicle(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) UpdateVehicle(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) DeleteVehicle(w http.ResponseWriter, r *http.Request, slug string) {}
