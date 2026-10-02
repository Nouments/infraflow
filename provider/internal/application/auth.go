package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/domain"
	"infraflow/provider/internal/ports"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserConflict       = errors.New("user already exists")
	ErrInvalidUser        = errors.New("invalid user")
	ErrLastAdmin          = errors.New("cannot disable or demote the last active administrator")
	ErrSessionNotFound    = errors.New("session not found")
)

const (
	minUsernameBytes = 3
	maxUsernameBytes = 64
	minPasswordBytes = 12
	maxPasswordBytes = 1024
)

type Authenticator struct {
	users      ports.UserRepository
	sessionTTL time.Duration
	dummyHash  []byte
}

func NewAuthenticator(users ports.UserRepository, sessionTTL time.Duration) (*Authenticator, error) {
	if users == nil {
		return nil, fmt.Errorf("user repository is required")
	}
	if sessionTTL < 5*time.Minute || sessionTTL > 24*time.Hour {
		return nil, fmt.Errorf("session TTL must be between 5 minutes and 24 hours")
	}
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("infraflow-invalid-login"), bcrypt.MinCost)
	if err != nil {
		return nil, fmt.Errorf("initialize password verifier: %w", err)
	}
	return &Authenticator{users: users, sessionTTL: sessionTTL, dummyHash: dummyHash}, nil
}

// EnsureBootstrap creates the first administrator only when the database is empty.
// An existing database never gets its administrator silently replaced.
func (auth *Authenticator) EnsureBootstrap(ctx context.Context, username, password string) error {
	if password == "" {
		_, _, err := auth.BootstrapAdmin(ctx, username)
		return err
	}
	count, err := auth.users.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return fmt.Errorf("bootstrap administrator credentials are required for a new database")
	}
	_, err = auth.CreateUser(ctx, username, password, domain.UserRoleAdmin)
	return err
}

// BootstrapAdmin creates the first administrator with a backend-generated
// password. Existing databases are never modified and return created=false.
func (auth *Authenticator) BootstrapAdmin(ctx context.Context, username string) (string, bool, error) {
	count, err := auth.users.CountUsers(ctx)
	if err != nil {
		return "", false, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return "", false, nil
	}
	password, err := randomToken(24)
	if err != nil {
		return "", false, fmt.Errorf("generate administrator password: %w", err)
	}
	if _, err := auth.CreateUser(ctx, username, password, domain.UserRoleAdmin); err != nil {
		return "", false, err
	}
	return password, true, nil
}

func (auth *Authenticator) Login(ctx context.Context, username, password string) (domain.UserSession, error) {
	username, err := validateUsername(username)
	if err != nil || len(password) > maxPasswordBytes {
		return domain.UserSession{}, ErrInvalidCredentials
	}
	credential, findErr := auth.users.FindUserByUsername(ctx, username)
	if findErr != nil {
		credential.PasswordHash = string(auth.dummyHash)
	}
	compareErr := bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password))
	if findErr != nil || compareErr != nil || credential.User.Disabled {
		return domain.UserSession{}, ErrInvalidCredentials
	}
	token, err := randomToken(32)
	if err != nil {
		return domain.UserSession{}, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(auth.sessionTTL)
	if err := auth.users.CreateSession(ctx, hashToken(token), ports.UserSessionRecord{UserID: credential.User.ID, ExpiresAt: expiresAt}); err != nil {
		return domain.UserSession{}, fmt.Errorf("persist session: %w", err)
	}
	return domain.UserSession{User: credential.User, Token: token, ExpiresAt: expiresAt}, nil
}

func (auth *Authenticator) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if len(token) < security.MinAgentTokenBytes || len(token) > 512 {
		return domain.User{}, ErrSessionNotFound
	}
	session, err := auth.users.FindSession(ctx, hashToken(token))
	if err != nil || !session.ExpiresAt.After(time.Now().UTC()) {
		if err == nil {
			_ = auth.users.DeleteSession(ctx, hashToken(token))
		}
		return domain.User{}, ErrSessionNotFound
	}
	user, err := auth.users.FindUserByID(ctx, session.UserID)
	if err != nil || user.Disabled {
		return domain.User{}, ErrSessionNotFound
	}
	return user, nil
}

func (auth *Authenticator) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := auth.users.DeleteSession(ctx, hashToken(token)); err != nil && !errors.Is(err, ErrSessionNotFound) {
		return err
	}
	return nil
}

func (auth *Authenticator) CreateUser(ctx context.Context, username, password string, role domain.UserRole) (domain.User, error) {
	username, err := validateUsername(username)
	if err != nil {
		return domain.User{}, err
	}
	if err := validatePassword(password); err != nil {
		return domain.User{}, err
	}
	if !validRole(role) {
		return domain.User{}, ErrInvalidUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().UTC()
	user := domain.User{ID: newUserID(), Username: username, Role: role, CreatedAt: now, UpdatedAt: now}
	if err := auth.users.CreateUser(ctx, user, string(hash)); err != nil {
		if errors.Is(err, ports.ErrConflict) {
			return domain.User{}, ErrUserConflict
		}
		return domain.User{}, err
	}
	return user, nil
}

func (auth *Authenticator) ListUsers(ctx context.Context) ([]domain.User, error) {
	return auth.users.ListUsers(ctx)
}

func (auth *Authenticator) User(ctx context.Context, id string) (domain.User, error) {
	user, err := auth.users.FindUserByID(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return domain.User{}, ErrUserNotFound
	}
	return user, err
}

func (auth *Authenticator) UpdateUser(ctx context.Context, user domain.User, password string) (domain.User, error) {
	if !protocol.ValidSiteName(user.ID) || !validRole(user.Role) {
		return domain.User{}, ErrInvalidUser
	}
	if password != "" {
		if err := validatePassword(password); err != nil {
			return domain.User{}, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return domain.User{}, fmt.Errorf("hash password: %w", err)
		}
		if err := auth.users.UpdateUser(ctx, user, string(hash)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return domain.User{}, ErrUserNotFound
			}
			return domain.User{}, err
		}
		if err := auth.users.RevokeUserSessions(ctx, user.ID); err != nil {
			return domain.User{}, fmt.Errorf("revoke user sessions: %w", err)
		}
		return user, nil
	}
	if !user.UpdatedAt.IsZero() {
		user.UpdatedAt = time.Now().UTC()
	}
	if err := auth.users.UpdateUser(ctx, user, ""); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.User{}, ErrUserNotFound
		}
		return domain.User{}, err
	}
	if err := auth.users.RevokeUserSessions(ctx, user.ID); err != nil {
		return domain.User{}, fmt.Errorf("revoke user sessions: %w", err)
	}
	return user, nil
}

func ValidateUserUpdate(user domain.User, password string) error {
	if !protocol.ValidSiteName(user.ID) || !validRole(user.Role) {
		return ErrInvalidUser
	}
	if password != "" {
		return validatePassword(password)
	}
	return nil
}

func (auth *Authenticator) EnsureAdminChangeAllowed(ctx context.Context, current domain.User, updated domain.User) error {
	if current.Role == domain.UserRoleAdmin && current.ID == updated.ID && (updated.Role != domain.UserRoleAdmin || updated.Disabled) {
		count, err := auth.users.CountActiveAdmins(ctx)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	return nil
}

func validateUsername(username string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if len(username) < minUsernameBytes || len(username) > maxUsernameBytes || !protocol.ValidSiteName(username) {
		return "", ErrInvalidUser
	}
	return username, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordBytes || len(password) > maxPasswordBytes {
		return fmt.Errorf("password must contain between %d and %d bytes", minPasswordBytes, maxPasswordBytes)
	}
	return nil
}

func validRole(role domain.UserRole) bool {
	return role == domain.UserRoleAdmin || role == domain.UserRoleUser
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func newUserID() string {
	token, err := randomToken(12)
	if err != nil {
		return "user-invalid"
	}
	return "user-" + token
}
