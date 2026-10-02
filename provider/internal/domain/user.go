package domain

import "time"

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      UserRole  `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserSession struct {
	User      User
	Token     string
	ExpiresAt time.Time
}
