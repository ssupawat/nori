package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

func openTestAuth(t *testing.T) *Service {
	t.Helper()
	db, err := clover.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open clover: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	svc, err := Open(db)
	if err != nil {
		t.Fatalf("open auth: %v", err)
	}
	return svc
}

func TestEnsureAdminIdempotent(t *testing.T) {
	// Arrange
	s := openTestAuth(t)

	// Act + Assert: the first ensure creates, later ensures never do.
	created, err := s.EnsureAdmin("admin", "secretpw1")
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	created, err = s.EnsureAdmin("admin", "secretpw1")
	if err != nil || created {
		t.Fatalf("second ensure should be a no-op: created=%v err=%v", created, err)
	}
	created, err = s.EnsureAdmin("root", "secretpw1")
	if err != nil || created {
		t.Fatalf("second admin should not be created: created=%v err=%v", created, err)
	}
}

func TestLoginAndValidate(t *testing.T) {
	// Arrange: one editor account.
	s := openTestAuth(t)
	if err := s.CreateUser("amy", "pw123456", Editor); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Act: log in with the right password, then validate the token.
	token, user, err := s.Login("amy", "pw123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Assert: the session resolves to the same user...
	if user.Role != Editor || user.Username != "amy" {
		t.Fatalf("unexpected user: %+v", user)
	}
	got, err := s.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got.Username != "amy" || got.Role != Editor {
		t.Fatalf("unexpected validated user: %+v", got)
	}

	// Act + Assert: wrong password and bogus tokens are rejected.
	if _, _, err := s.Login("amy", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if _, err := s.Validate("bogus"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogoutInvalidatesToken(t *testing.T) {
	// Arrange: a logged-in viewer.
	s := openTestAuth(t)
	if err := s.CreateUser("amy", "pw123456", Viewer); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := s.Login("amy", "pw123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Act: log out.
	if err := s.Logout(token); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// Assert: the token no longer validates.
	if _, err := s.Validate(token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials after logout, got %v", err)
	}
}

func TestCreateUserValidation(t *testing.T) {
	// Arrange
	s := openTestAuth(t)

	// Act + Assert: each bad input is rejected, a good one creates, and a
	// duplicate name reports ErrUserExists.
	if err := s.CreateUser("", "password1", Editor); err == nil {
		t.Fatal("expected error for empty username")
	}
	if err := s.CreateUser("amy", "", Editor); err == nil {
		t.Fatal("expected error for empty password")
	}
	if err := s.CreateUser("amy", "short", Editor); err == nil {
		t.Fatal("expected error for short password")
	}
	if err := s.CreateUser("amy", "pw", Role("boss")); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
	if err := s.CreateUser("amy", "password1", Viewer); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.CreateUser("amy", "password2", Viewer); !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected ErrUserExists, got %v", err)
	}
}

func TestDeleteUserGuards(t *testing.T) {
	// Arrange: one admin plus one editor.
	s := openTestAuth(t)
	if _, err := s.EnsureAdmin("admin", "secretpw1"); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}
	if err := s.CreateUser("amy", "pw123456", Editor); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Act + Assert: the last admin cannot be deleted...
	if err := s.DeleteUser("admin"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}

	// Act + Assert: ...but with a second admin present, deletion works.
	if err := s.CreateUser("admin2", "secretpw1", Admin); err != nil {
		t.Fatalf("create second admin: %v", err)
	}
	if err := s.DeleteUser("admin"); err != nil {
		t.Fatalf("delete admin with another admin present: %v", err)
	}

	// Arrange: amy holds a live session.
	token, _, err := s.Login("amy", "pw123456")
	if err != nil {
		t.Fatalf("login amy: %v", err)
	}

	// Act: delete amy.
	if err := s.DeleteUser("amy"); err != nil {
		t.Fatalf("delete amy: %v", err)
	}

	// Assert: her sessions died with the account, and a repeat delete
	// reports the user as missing.
	if _, err := s.Validate(token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("sessions of deleted user should die, got %v", err)
	}
	if err := s.DeleteUser("amy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListUsers(t *testing.T) {
	// Arrange: one admin, one viewer, one editor.
	s := openTestAuth(t)
	if _, err := s.EnsureAdmin("admin", "secretpw1"); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}
	if err := s.CreateUser("amy", "pw123456", Viewer); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := s.CreateUser("bob", "pw123456", Editor); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Act: list the users.
	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	// Assert: admins first, then editors, then viewers - usernames
	// alphabetical within a role.
	if len(users) != 3 {
		t.Fatalf("users: %+v", users)
	}
	want := []string{"admin", "bob", "amy"}
	for i, u := range users {
		if u.Username != want[i] {
			t.Fatalf("user %d = %s, want %s", i, u.Username, want[i])
		}
	}
}

func TestSessionSlidingRenewal(t *testing.T) {
	// Arrange: an admin with a live session, aged into its second half.
	s := openTestAuth(t)
	if _, err := s.EnsureAdmin("admin", "secretpw1"); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}
	token, _, err := s.Login("admin", "secretpw1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := ageSession(s, token, time.Hour); err != nil {
		t.Fatalf("age session: %v", err)
	}

	// Act: validate the aged session.
	if _, err := s.Validate(token); err != nil {
		t.Fatalf("validate aged session: %v", err)
	}

	// Assert: Validate extended it back to ~full TTL.
	expires := s.sessionExpiry(token)
	if until := time.Until(expires); until < sessionTTL-time.Hour {
		t.Fatalf("session not renewed: %v left", until)
	}

	// Act: validate a fresh session (full TTL left).
	expiresBefore := s.sessionExpiry(token)
	if _, err := s.Validate(token); err != nil {
		t.Fatalf("validate fresh session: %v", err)
	}

	// Assert: the fresh session was not rewritten.
	if !s.sessionExpiry(token).Equal(expiresBefore) {
		t.Fatal("fresh session was rewritten on validate")
	}
}

// ageSession moves the session's expiry to now+remaining.
func ageSession(s *Service, token string, remaining time.Duration) error {
	return s.db.UpdateById(sessionsCollection, s.sessionID(token), func(doc *d.Document) *d.Document {
		doc.Set("expiresAt", time.Now().Add(remaining))
		return doc
	})
}

func (s *Service) sessionID(token string) string {
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		t, _ := doc.Get("token").(string)
		return t == token
	})
	doc, err := s.db.FindFirst(q)
	if err != nil || doc == nil {
		return ""
	}
	return doc.ObjectId()
}

func (s *Service) sessionExpiry(token string) time.Time {
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		t, _ := doc.Get("token").(string)
		return t == token
	})
	doc, err := s.db.FindFirst(q)
	if err != nil || doc == nil {
		return time.Time{}
	}
	return parseTime(doc.Get("expiresAt"))
}

func TestAPIKeys(t *testing.T) {
	// Arrange: a service with its bootstrap admin.
	s := openTestAuth(t)
	if _, err := s.EnsureAdmin("admin", "secretpw1"); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}

	// Act: mint an editor key and validate it.
	full, key, err := s.CreateAPIKey("my-ai-agent", Editor)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	// Assert: the key has the documented shape...
	if !strings.HasPrefix(full, "nri_") {
		t.Fatalf("key format: %q", full)
	}
	if key.Prefix == full || key.ID == "" {
		t.Fatalf("key metadata: %+v", key)
	}
	// ...validation resolves the role and carries the key's name for
	// attribution...
	u, err := s.ValidateAPIKey(full)
	if err != nil || u.Role != Editor || u.Username != "key:my-ai-agent" {
		t.Fatalf("validate: user=%+v err=%v", u, err)
	}
	// ...and wrong or non-key tokens are rejected.
	if _, err := s.ValidateAPIKey(full + "x"); err == nil {
		t.Fatal("expected invalid key")
	}
	if _, err := s.ValidateAPIKey("sess-looks-like-session"); err == nil {
		t.Fatal("expected non-key token to fail key validation")
	}

	// Act: use the key, then read the key list.
	_, _ = s.ValidateAPIKey(full)
	keys, _ := s.ListAPIKeys()

	// Assert: lastUsedAt got stamped.
	if len(keys) != 1 || keys[0].LastUsedAt == "" {
		t.Fatalf("list: %+v", keys)
	}

	// Act: revoke the key, then revoke it again.
	if err := s.DeleteAPIKey(key.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Assert: deleting a missing key reports it, same as deleting a
	// missing user, and the revoked key no longer validates.
	if err := s.DeleteAPIKey(key.ID); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("delete missing key: %v", err)
	}
	if _, err := s.ValidateAPIKey(full); err == nil {
		t.Fatal("revoked key still validates")
	}
}

func TestPasswordMinLength(t *testing.T) {
	// Arrange
	s := openTestAuth(t)
	if _, err := s.EnsureAdmin("admin", "secretpw1"); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}

	// Act + Assert: a short password is rejected, a long enough one is not.
	if err := s.CreateUser("shorty", "abc", Viewer); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	if err := s.CreateUser("ok", "longenough", Viewer); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
}
