package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `db:"id"`
	Email        string     `db:"email"`
	Slug         string     `db:"slug"`
	PasswordHash string     `db:"password_hash"`
	FullName     string     `db:"full_name"`
	AvatarURL    string     `db:"avatar_url"`
	Role         string     `db:"role"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	LastLoginAt  *time.Time `db:"last_login_at"`
}
