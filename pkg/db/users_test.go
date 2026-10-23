package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"cockpit/pkg/auth"
)

func TestTokenStorageHashingAndLegacyMigration(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	user, err := s.CreateUser(ctx, "alice", "password-hash", "Alice", auth.RoleViewer, now)
	if err != nil {
		t.Fatal(err)
	}
	const rawToken = "new-browser-token"
	if err := s.IssueToken(ctx, user.ID, rawToken, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT token FROM api_tokens WHERE user_id = ?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == rawToken || stored != auth.HashToken(rawToken) {
		t.Fatalf("stored token is not the SHA-256 digest: %q", stored)
	}
	if _, err := s.UserByToken(ctx, rawToken, now); err != nil {
		t.Fatalf("hashed token lookup: %v", err)
	}

	const legacyToken = "legacy-plaintext-token"
	if _, err := s.db.Exec(`INSERT INTO api_tokens (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		legacyToken, user.ID, millis(now), millis(now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByToken(ctx, legacyToken, now); err != nil {
		t.Fatalf("legacy token lookup: %v", err)
	}
	var legacyRawRemaining, legacyHashCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE token = ?`, legacyToken).Scan(&legacyRawRemaining); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE token = ?`, auth.HashToken(legacyToken)).Scan(&legacyHashCount); err != nil {
		t.Fatal(err)
	}
	if legacyRawRemaining != 0 || legacyHashCount != 1 {
		t.Fatalf("legacy token was not migrated: raw=%d hashed=%d", legacyRawRemaining, legacyHashCount)
	}
	if err := s.DeleteToken(ctx, legacyToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByToken(ctx, legacyToken, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("deleted token lookup error = %v", err)
	}
}

func TestPasswordResetRevokesAllSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	user, err := s.CreateUser(ctx, "alice", "old-hash", "Alice", auth.RoleViewer, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"session-one", "session-two"} {
		if err := s.IssueToken(ctx, user.ID, token, now, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	updated, err := s.SetUserPassword(ctx, user.ID, "new-hash")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordHash != "new-hash" {
		t.Fatalf("password hash = %q", updated.PasswordHash)
	}
	for _, token := range []string{"session-one", "session-two"} {
		if _, err := s.UserByToken(ctx, token, now); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("session %q survived password reset: %v", token, err)
		}
	}
}
