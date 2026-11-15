package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func (f *fixture) user(t *testing.T, email string, role int) *User {
	t.Helper()
	u, err := f.s.CreateUser(context.Background(), f.ws.ID, email, "password123", "", role)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *fixture) session(t *testing.T, u *User) string {
	t.Helper()
	tok, err := f.s.CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (f *fixture) loggedIn(tok string) bool {
	_, err := f.s.SessionUser(context.Background(), tok)
	return err == nil
}

func TestChangePassword(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	keep, other := f.session(t, f.admin), f.session(t, f.admin)
	_, secret, err := f.s.CreateToken(ctx, f.admin.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}

	err = f.s.ChangePassword(ctx, f.admin.ID, "wrong-password", "newpassword1", keep)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalid || e.Field != "current_password" {
		t.Fatalf("wrong current password: %v", err)
	}
	if kind(f.s.ChangePassword(ctx, f.admin.ID, "password123", "short", keep)) != KindInvalid {
		t.Fatal("short new password accepted")
	}
	if !f.loggedIn(other) {
		t.Fatal("failed change must not revoke sessions")
	}

	if err := f.s.ChangePassword(ctx, f.admin.ID, "password123", "newpassword1", keep); err != nil {
		t.Fatal(err)
	}
	if !f.loggedIn(keep) {
		t.Error("current session was revoked")
	}
	if f.loggedIn(other) {
		t.Error("other session survived")
	}
	if _, err := f.s.Authenticate(ctx, "admin@example.com", "password123"); !errors.Is(err, ErrBadLogin) {
		t.Errorf("old password still works: %v", err)
	}
	if _, err := f.s.Authenticate(ctx, "admin@example.com", "newpassword1"); err != nil {
		t.Errorf("new password: %v", err)
	}
	if _, err := f.s.TokenUser(ctx, secret); err != nil {
		t.Errorf("API token affected: %v", err)
	}
}

func TestSetMemberDeactivate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	u := f.user(t, "m@example.com", RoleMember)
	tok := f.session(t, u)
	_, secret, _ := f.s.CreateToken(ctx, u.ID, "ci")

	m, err := f.s.SetMember(ctx, f.ws.ID, u.ID, Opt[int]{}, Some(false))
	if err != nil {
		t.Fatal(err)
	}
	if m.IsActive || m.Role != RoleMember {
		t.Errorf("member = active %v role %d", m.IsActive, m.Role)
	}
	if f.loggedIn(tok) {
		t.Error("session survived deactivation")
	}
	if _, err := f.s.Authenticate(ctx, "m@example.com", "password123"); !errors.Is(err, ErrBadLogin) {
		t.Errorf("Authenticate: %v", err)
	}
	if _, err := f.s.TokenUser(ctx, secret); !errors.Is(err, ErrNotFound) {
		t.Errorf("TokenUser: %v", err)
	}

	m, err = f.s.SetMember(ctx, f.ws.ID, u.ID, Some(RoleGuest), Some(true))
	if err != nil || !m.IsActive || m.Role != RoleGuest {
		t.Fatalf("reactivate: %v %+v", err, m)
	}
	if _, err := f.s.Authenticate(ctx, "m@example.com", "password123"); err != nil {
		t.Errorf("Authenticate after reactivation: %v", err)
	}
	if _, err := f.s.SetMember(ctx, f.ws.ID, u.ID, Some(7), Opt[bool]{}); kind(err) != KindInvalid {
		t.Errorf("bad role: %v", err)
	}
	if _, err := f.s.SetMember(ctx, f.ws.ID, "no-such-user", Some(RoleGuest), Opt[bool]{}); kind(err) != KindNotFound {
		t.Errorf("unknown user: %v", err)
	}
}

func TestLastAdminGuard(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	refused := func(what string, err error) {
		t.Helper()
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindInvalid || e.Msg != "The workspace needs at least one active admin." {
			t.Errorf("%s: err = %v", what, err)
		}
	}
	_, err := f.s.SetMember(ctx, f.ws.ID, f.admin.ID, Some(RoleMember), Opt[bool]{})
	refused("self-demote", err)
	_, err = f.s.SetMember(ctx, f.ws.ID, f.admin.ID, Opt[int]{}, Some(false))
	refused("deactivate last admin", err)
	// A refused change leaves everything as it was.
	if role, _ := f.s.MemberRole(ctx, f.ws.ID, f.admin.ID); role != RoleAdmin {
		t.Errorf("role after refusal = %d", role)
	}
	if _, err := f.s.Authenticate(ctx, "admin@example.com", "password123"); err != nil {
		t.Errorf("admin locked out after refusal: %v", err)
	}

	// An inactive admin does not count.
	second := f.user(t, "b@example.com", RoleAdmin)
	if _, err := f.s.SetMember(ctx, f.ws.ID, second.ID, Opt[int]{}, Some(false)); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.SetMember(ctx, f.ws.ID, f.admin.ID, Some(RoleMember), Opt[bool]{})
	refused("demote with only an inactive second admin", err)

	if _, err := f.s.SetMember(ctx, f.ws.ID, second.ID, Opt[int]{}, Some(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetMember(ctx, f.ws.ID, f.admin.ID, Some(RoleMember), Opt[bool]{}); err != nil {
		t.Errorf("demote with a second active admin: %v", err)
	}
	_, err = f.s.SetMember(ctx, f.ws.ID, second.ID, Some(RoleGuest), Opt[bool]{})
	refused("demote the new last admin", err)
}

func TestPurgeExpiredSessions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	live := f.session(t, f.admin)
	ts := func(d time.Duration) string { return time.Now().UTC().Add(d).Format(TimeLayout) }
	for _, row := range []struct {
		tok string
		exp string
	}{{"old-1", ts(-time.Hour)}, {"old-2", ts(-time.Second)}} {
		_, err := f.s.w.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
			hashToken(row.tok), f.admin.ID, ts(-2*time.Hour), row.exp)
		if err != nil {
			t.Fatal(err)
		}
	}
	n, err := f.s.PurgeExpiredSessions(ctx)
	if err != nil || n != 2 {
		t.Fatalf("purged %d, %v; want 2", n, err)
	}
	if !f.loggedIn(live) {
		t.Error("live session purged")
	}
	if n, _ := f.s.PurgeExpiredSessions(ctx); n != 0 {
		t.Errorf("second purge = %d", n)
	}
}
