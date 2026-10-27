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

func TestLastAdminCannotBeRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	root, err := s.CreateUser(ctx, "root", "hash", "Root", auth.RoleAdmin, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetUserRole(ctx, root.ID, auth.RoleViewer); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote last admin: err = %v, want ErrLastAdmin", err)
	}
	if err := s.DeleteUser(ctx, root.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete last admin: err = %v, want ErrLastAdmin", err)
	}

	second, err := s.CreateUser(ctx, "root2", "hash", "Root2", auth.RoleAdmin, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetUserRole(ctx, root.ID, auth.RoleViewer); err != nil {
		t.Fatalf("demote with a spare admin: %v", err)
	}
	// root is now a viewer and "second" is the only admin again.
	if err := s.DeleteUser(ctx, second.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete the remaining admin: err = %v, want ErrLastAdmin", err)
	}
	third, err := s.CreateUser(ctx, "root3", "hash", "Root3", auth.RoleAdmin, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, second.ID); err != nil {
		t.Fatalf("delete one of two admins: %v", err)
	}
	if n, err := s.CountUsersWithRole(ctx, auth.RoleAdmin); err != nil || n != 1 {
		t.Fatalf("admin count = %d (%v), want 1", n, err)
	}
	if _, err := s.SetUserRole(ctx, third.ID, auth.RoleOperator); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote the remaining admin: err = %v, want ErrLastAdmin", err)
	}
	if _, err := s.SetUserRole(ctx, 999999, auth.RoleViewer); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user role change: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteUser(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user delete: err = %v, want ErrNotFound", err)
	}
}

func TestAddUserToDepartmentRequiresBothRecords(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	user, err := s.CreateUser(ctx, "alice", "hash", "Alice", auth.RoleViewer, now)
	if err != nil {
		t.Fatal(err)
	}
	dept, err := s.CreateDepartment(ctx, "Ops", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddUserToDepartment(ctx, 424242, dept.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: err = %v, want ErrNotFound", err)
	}
	if err := s.AddUserToDepartment(ctx, user.ID, 424242); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown department: err = %v, want ErrNotFound", err)
	}
	if err := s.AddUserToDepartment(ctx, user.ID, dept.ID); err != nil {
		t.Fatalf("valid membership: %v", err)
	}
	if err := s.AddUserToDepartment(ctx, user.ID, dept.ID); err != nil {
		t.Fatalf("idempotent membership: %v", err)
	}
}

func TestDepartmentPermissionRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	user, err := s.CreateUser(ctx, "alice", "hash", "Alice", auth.RoleViewer, now)
	if err != nil {
		t.Fatal(err)
	}
	dept, err := s.CreateDepartment(ctx, "SC desk", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddUserToDepartment(ctx, user.ID, dept.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDepartmentPermissions(ctx, dept.ID, []string{"manuals:approve", "customers:manage"}); err != nil {
		t.Fatal(err)
	}
	perms, err := s.UserDepartmentPermissions(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range perms {
		got[p] = true
	}
	if !got["manuals:approve"] || !got["customers:manage"] || len(perms) != 2 {
		t.Fatalf("effective department permissions = %v", perms)
	}
	stored, err := s.DepartmentByID(ctx, dept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Permissions) != 2 {
		t.Fatalf("stored permissions = %v", stored.Permissions)
	}
}
