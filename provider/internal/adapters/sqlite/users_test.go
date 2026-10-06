package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"infraflow/provider/internal/application"
	"infraflow/provider/internal/domain"
)

func TestStorePersistsUsersAndSessions(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "users.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, err := application.NewAuthenticator(store, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := auth.EnsureBootstrap(ctx, "Admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureBootstrap(ctx, "other", "another correct password"); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx, "admin", "correct horse battery staple")
	if err != nil || session.Token == "" || session.User.Role != domain.UserRoleAdmin {
		t.Fatalf("administrator login failed: %#v, %v", session, err)
	}
	if _, err := auth.Login(ctx, "admin", "wrong password"); !errors.Is(err, application.ErrInvalidCredentials) {
		t.Fatalf("wrong password was accepted: %v", err)
	}
	user, err := auth.CreateUser(ctx, "operator", "operator password 123", domain.UserRoleUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateUser(ctx, "OPERATOR", "another password 123", domain.UserRoleUser); !errors.Is(err, application.ErrUserConflict) {
		t.Fatalf("duplicate username was not rejected: %v", err)
	}
	if authenticated, err := auth.Authenticate(ctx, session.Token); err != nil || authenticated.ID != session.User.ID {
		t.Fatalf("session authentication failed: %#v, %v", authenticated, err)
	}
	if err := auth.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, session.Token); !errors.Is(err, application.ErrSessionNotFound) {
		t.Fatalf("logged out session remained valid: %v", err)
	}
	if listed, err := auth.ListUsers(ctx); err != nil || len(listed) != 2 {
		t.Fatalf("unexpected persisted users: %#v, %v", listed, err)
	}
	if user.Username != "operator" {
		t.Fatalf("username was not normalized: %#v", user)
	}
}

func TestAuthenticatorProtectsLastAdministrator(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "users.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, err := application.NewAuthenticator(store, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := auth.EnsureBootstrap(ctx, "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	admin, err := auth.User(ctx, "user-does-not-exist")
	if !errors.Is(err, application.ErrUserNotFound) || admin.ID != "" {
		t.Fatalf("missing user lookup returned %#v, %v", admin, err)
	}
	users, err := auth.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureAdminChangeAllowed(ctx, users[0], domain.User{ID: users[0].ID, Role: domain.UserRoleUser, Disabled: true}); !errors.Is(err, application.ErrLastAdmin) {
		t.Fatalf("last administrator change was allowed: %v", err)
	}
}
