package handler

import "net/http"

func (s *Server) ListManagers(w http.ResponseWriter, r *http.Request) {}

func (s *Server) CreateManager(w http.ResponseWriter, r *http.Request) {}

func (s *Server) GetManager(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) DeleteManager(w http.ResponseWriter, r *http.Request, slug string) {}
