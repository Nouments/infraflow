package ports

import (
	"context"
	"errors"
	"time"

	"infraflow/provider/internal/domain"
)

var ErrConflict = errors.New("repository conflict")

type UserCredential struct {
	User         domain.User
	PasswordHash string
}

type UserSessionRecord struct {
	UserID    string
	ExpiresAt time.Time
}

type UserRepository interface {
	CountUsers(context.Context) (int, error)
	CountActiveAdmins(context.Context) (int, error)
	FindUserByUsername(context.Context, string) (UserCredential, error)
	FindUserByID(context.Context, string) (domain.User, error)
	ListUsers(context.Context) ([]domain.User, error)
	CreateUser(context.Context, domain.User, string) error
	UpdateUser(context.Context, domain.User, string) error
	CreateSession(context.Context, string, UserSessionRecord) error
	FindSession(context.Context, string) (UserSessionRecord, error)
	DeleteSession(context.Context, string) error
	RevokeUserSessions(context.Context, string) error
	Close() error
}
