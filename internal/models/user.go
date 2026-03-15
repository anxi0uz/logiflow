package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `db:"id"            json:"id"`
	Email        string     `db:"email"         json:"email"`
	Slug         string     `db:"slug"          json:"slug"`
	PasswordHash string     `db:"password_hash" json:"-"`
	FullName     *string    `db:"full_name"     json:"full_name,omitempty"`
	AvatarURL    *string    `db:"avatar_url"    json:"avatar_url,omitempty"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
	UpdatedAt    *time.Time `db:"updated_at"    json:"updated_at,omitempty"`
	LastLoginAt  *time.Time `db:"last_login_at" json:"last_login_at,omitempty"`
}
