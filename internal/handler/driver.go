package handler

import "net/http"

func (s *Server) ListDrivers(w http.ResponseWriter, r *http.Request) {}

func (s *Server) CreateDriver(w http.ResponseWriter, r *http.Request) {}

func (s *Server) GetDriver(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) UpdateDriver(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) DeleteDriver(w http.ResponseWriter, r *http.Request, slug string) {}
