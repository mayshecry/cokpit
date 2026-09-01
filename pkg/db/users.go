package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cockpit/pkg/auth"
)

var ErrDuplicateUsername = errors.New("username already exists")

var ErrInvalidToken = errors.New("invalid or expired token")

func scanUser(row interface{ Scan(...any) error }) (auth.User, error) {
	var u auth.User
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &created); err != nil {
		return auth.User{}, err
	}
	u.CreatedAt = fromMillis(created)
	return u, nil
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, displayName string, role auth.Role, now time.Time) (auth.User, error) {
	if username == "" || passwordHash == "" {
		return auth.User{}, errors.New("username and password hash are required")
	}
	if !auth.IsValidRole(role) {
		return auth.User{}, errors.New("invalid role")
	}

	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&exists); err != nil {
		return auth.User{}, fmt.Errorf("check duplicate username: %w", err)
	}
	if exists > 0 {
		return auth.User{}, ErrDuplicateUsername
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, display_name, role, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		username, passwordHash, displayName, string(role), millis(now))
	if err != nil {
		return auth.User{}, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return auth.User{}, fmt.Errorf("last insert id: %w", err)
	}
	return auth.User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		Role:         role,
		CreatedAt:    now.UTC(),
	}, nil
}

func (s *Store) EnsureUser(ctx context.Context, username, passwordHash, displayName string, role auth.Role, now time.Time) (auth.User, error) {
	existing, err := s.UserByUsername(ctx, username)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return auth.User{}, err
	}
	return s.CreateUser(ctx, username, passwordHash, displayName, role, now)
}

func (s *Store) UserByUsername(ctx context.Context, username string) (auth.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, display_name, role, created_at
		 FROM users WHERE username = ?`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("user by username: %w", err)
	}
	return u, nil
}

func (s *Store) UserByID(ctx context.Context, id int64) (auth.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, display_name, role, created_at
		 FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("user by id: %w", err)
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]auth.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, password_hash, display_name, role, created_at
		 FROM users ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := []auth.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (s *Store) SetUserRole(ctx context.Context, id int64, role auth.Role) (auth.User, error) {
	if !auth.IsValidRole(role) {
		return auth.User{}, errors.New("invalid role")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET role = ? WHERE id = ?`, string(role), id)
	if err != nil {
		return auth.User{}, fmt.Errorf("update role: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return auth.User{}, ErrNotFound
	}
	return s.UserByID(ctx, id)
}

func (s *Store) SetUserPassword(ctx context.Context, id int64, passwordHash string) (auth.User, error) {
	if passwordHash == "" {
		return auth.User{}, errors.New("password hash is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return auth.User{}, fmt.Errorf("update password: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return auth.User{}, ErrNotFound
	}
	return s.UserByID(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) IssueToken(ctx context.Context, userID int64, token string, now, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, millis(now), millis(expiresAt))
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

func (s *Store) UserByToken(ctx context.Context, token string, now time.Time) (auth.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.display_name, u.role, u.created_at
		 FROM api_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token = ? AND t.expires_at > ?`, token, millis(now)))
	if errors.Is(err, ErrNotFound) {
		return auth.User{}, ErrInvalidToken
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("lookup token: %w", err)
	}
	return u, nil
}

func (s *Store) DeleteToken(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE token = ?`, token); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}
