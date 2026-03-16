package handler

import (
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	ID      uuid.UUID `json:"id"`
	Email   string    `json:"email"`
	Role    string    `json:"role"`
	TokenID string    `json:"token_id"`
	jwt.RegisteredClaims
}

func (s *Server) AuthLogin(w http.ResponseWriter, r *http.Request) {

}
func (s *Server) AuthLogout(w http.ResponseWriter, r *http.Request)   {}
func (s *Server) AuthRefresh(w http.ResponseWriter, r *http.Request)  {}
func (s *Server) AuthRegister(w http.ResponseWriter, r *http.Request) {}
func (s *Server) DeleteMe(w http.ResponseWriter, r *http.Request)     {}
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request)        {}
func (s *Server) UpdateMe(w http.ResponseWriter, r *http.Request)     {}
