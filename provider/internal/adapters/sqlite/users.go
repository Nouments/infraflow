package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mattn/go-sqlite3"

	"infraflow/provider/internal/domain"
	"infraflow/provider/internal/ports"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);
`

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("SQLite database path is required")
	}
	if parent := filepath.Dir(path); parent != "." {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, fmt.Errorf("create SQLite database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	store := &Store{db: db}
	if _, err := store.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure SQLite database: %w", err)
	}
	if _, err := store.db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize SQLite schema: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("restrict SQLite database permissions: %w", err)
	}
	return store, nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (store *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active administrators: %w", err)
	}
	return count, nil
}

func (store *Store) FindUserByUsername(ctx context.Context, username string) (ports.UserCredential, error) {
	row := store.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, disabled, created_at, updated_at FROM users WHERE username = ?`, username)
	return scanCredential(row)
}

func (store *Store) FindUserByID(ctx context.Context, id string) (domain.User, error) {
	row := store.db.QueryRowContext(ctx, `SELECT id, username, role, disabled, created_at, updated_at FROM users WHERE id = ?`, id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, os.ErrNotExist
	}
	return user, err
}

func (store *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT id, username, role, disabled, created_at, updated_at FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("decode user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (store *Store) CreateUser(ctx context.Context, user domain.User, passwordHash string) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, role, disabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username, passwordHash, user.Role, boolInt(user.Disabled), user.CreatedAt.UTC().Format(time.RFC3339Nano), user.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if isUniqueError(err) {
		return ports.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (store *Store) UpdateUser(ctx context.Context, user domain.User, passwordHash string) error {
	var result sql.Result
	var err error
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if passwordHash == "" {
		result, err = store.db.ExecContext(ctx, `UPDATE users SET role = ?, disabled = ?, updated_at = ? WHERE id = ?`, user.Role, boolInt(user.Disabled), updatedAt, user.ID)
	} else {
		result, err = store.db.ExecContext(ctx, `UPDATE users SET role = ?, disabled = ?, password_hash = ?, updated_at = ? WHERE id = ?`, user.Role, boolInt(user.Disabled), passwordHash, updatedAt, user.ID)
	}
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect updated user: %w", err)
	}
	if count != 1 {
		return os.ErrNotExist
	}
	user.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return nil
}

func (store *Store) CreateSession(ctx context.Context, tokenHash string, session ports.UserSessionRecord) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, tokenHash, session.UserID, session.ExpiresAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (store *Store) FindSession(ctx context.Context, tokenHash string) (ports.UserSessionRecord, error) {
	var session ports.UserSessionRecord
	var expiresAt string
	err := store.db.QueryRowContext(ctx, `SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&session.UserID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.UserSessionRecord{}, os.ErrNotExist
	}
	if err != nil {
		return ports.UserSessionRecord{}, fmt.Errorf("find session: %w", err)
	}
	session.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return ports.UserSessionRecord{}, fmt.Errorf("decode session expiry: %w", err)
	}
	return session, nil
}

func (store *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := store.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (store *Store) RevokeUserSessions(ctx context.Context, userID string) error {
	if _, err := store.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(...any) error
}

func scanCredential(row scanner) (ports.UserCredential, error) {
	var user domain.User
	var passwordHash, role, createdAt, updatedAt string
	var disabled int
	if err := row.Scan(&user.ID, &user.Username, &passwordHash, &role, &disabled, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ports.UserCredential{}, os.ErrNotExist
		}
		return ports.UserCredential{}, fmt.Errorf("decode user credential: %w", err)
	}
	if err := fillUser(&user, role, disabled, createdAt, updatedAt); err != nil {
		return ports.UserCredential{}, err
	}
	return ports.UserCredential{User: user, PasswordHash: passwordHash}, nil
}

func scanUser(row scanner) (domain.User, error) {
	var user domain.User
	var role, createdAt, updatedAt string
	var disabled int
	if err := row.Scan(&user.ID, &user.Username, &role, &disabled, &createdAt, &updatedAt); err != nil {
		return domain.User{}, err
	}
	if err := fillUser(&user, role, disabled, createdAt, updatedAt); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func fillUser(user *domain.User, role string, disabled int, createdAt, updatedAt string) error {
	user.Role = domain.UserRole(role)
	user.Disabled = disabled == 1
	var err error
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return fmt.Errorf("decode user creation time: %w", err)
	}
	user.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return fmt.Errorf("decode user update time: %w", err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isUniqueError(err error) bool {
	var sqliteErr sqlite3.Error
	return err != nil && errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint
}
