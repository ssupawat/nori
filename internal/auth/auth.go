package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

const (
	usersCollection    = "_users"
	sessionsCollection = "_sessions"
	sessionTTL         = 7 * 24 * time.Hour
	loginDelay         = 500 * time.Millisecond
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrNotFound           = errors.New("user not found")
	ErrUserExists         = errors.New("username already taken")
	ErrLastAdmin          = errors.New("cannot delete the last admin")
	ErrInvalidRole        = errors.New("invalid role")
	ErrKeyNotFound        = errors.New("API key not found")
	ErrKeyExists          = errors.New("API key name already taken")
	ErrInvalidInput       = errors.New("invalid input")
)

// inputError is a readable message that matches ErrInvalidInput, so the
// API answers 400 for bad input and 500 for storage failures.
type inputError string

func (e inputError) Error() string        { return string(e) }
func (e inputError) Is(target error) bool { return target == ErrInvalidInput }

var errShortPassword = inputError("password must be at least 8 characters")

type Role string

const (
	Admin   Role = "admin"
	Editor  Role = "editor"
	Viewer  Role = "viewer"
	Builder Role = "builder"
)

func (r Role) Valid() bool {
	return r == Admin || r == Editor || r == Viewer || r == Builder
}

type User struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
}

type Service struct {
	db        *clover.DB
	keyMu     sync.Mutex // makes the key-name uniqueness check and insert one step
	purgeMu   sync.Mutex
	lastPurge time.Time
}

func Open(db *clover.DB) (*Service, error) {
	for _, c := range []string{usersCollection, sessionsCollection, keysCollection} {
		exists, err := db.HasCollection(c)
		if err != nil {
			return nil, err
		}
		if !exists {
			if err := db.CreateCollection(c); err != nil {
				return nil, err
			}
		}
	}
	return &Service{db: db}, nil
}

// EnsureAdmin creates the bootstrap admin account if no admin exists yet.
// It returns true when the account was created.
func (s *Service) EnsureAdmin(username, password string) (bool, error) {
	if strings.TrimSpace(username) == "" || password == "" {
		return false, fmt.Errorf("admin username and password must not be empty")
	}
	existing, err := s.findUserDoc(username)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	hasAdmin, err := s.HasAdmin()
	if err != nil {
		return false, err
	}
	if hasAdmin {
		return false, nil
	}
	return true, s.CreateUser(username, password, Admin)
}

// HasAdmin reports whether an admin account exists.
func (s *Service) HasAdmin() (bool, error) {
	q := query.NewQuery(usersCollection).MatchFunc(func(doc *d.Document) bool {
		return Role(doc.Get("role").(string)) == Admin
	})
	return s.db.Exists(q)
}

// Login verifies credentials and returns a new session token.
func (s *Service) Login(username, password string) (token string, user User, err error) {
	doc, err := s.findUserDoc(username)
	if err != nil {
		return "", User{}, err
	}
	if doc == nil {
		time.Sleep(loginDelay)
		return "", User{}, ErrInvalidCredentials
	}
	hash, _ := doc.Get("hash").(string)
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		time.Sleep(loginDelay)
		return "", User{}, ErrInvalidCredentials
	}
	s.purgeExpiredSessions()

	token, err = newToken()
	if err != nil {
		return "", User{}, err
	}
	doc2 := d.NewDocument()
	doc2.Set("token", token)
	doc2.Set("username", username)
	doc2.Set("expiresAt", time.Now().Add(sessionTTL))
	if _, err := s.db.InsertOne(sessionsCollection, doc2); err != nil {
		return "", User{}, err
	}
	return token, User{Username: username, Role: Role(doc.Get("role").(string))}, nil
}

// maybePurgeExpiredSessions runs the purge at most once an hour, so idle
// instances also shed dead sessions.
func (s *Service) maybePurgeExpiredSessions() {
	s.purgeMu.Lock()
	due := time.Since(s.lastPurge) >= time.Hour
	if due {
		s.lastPurge = time.Now()
	}
	s.purgeMu.Unlock()
	if due {
		go s.purgeExpiredSessions()
	}
}

// Validate resolves a session token to its user, if the session is alive.
func (s *Service) Validate(token string) (User, error) {
	if token == "" {
		return User{}, ErrInvalidCredentials
	}
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		t, _ := doc.Get("token").(string)
		return t == token
	})
	doc, err := s.db.FindFirst(q)
	if err != nil {
		return User{}, err
	}
	if doc == nil {
		return User{}, ErrInvalidCredentials
	}
	s.maybePurgeExpiredSessions()
	expiresAt := parseTime(doc.Get("expiresAt"))
	if !expiresAt.IsZero() && time.Now().After(expiresAt) {
		_ = s.db.DeleteById(sessionsCollection, doc.ObjectId())
		return User{}, ErrInvalidCredentials
	}
	// sliding renewal: extend once past half its life, so active users are
	// only logged out after a full TTL of inactivity
	if !expiresAt.IsZero() && time.Until(expiresAt) < sessionTTL/2 {
		_ = s.db.UpdateById(sessionsCollection, doc.ObjectId(), func(doc *d.Document) *d.Document {
			doc.Set("expiresAt", time.Now().Add(sessionTTL))
			return doc
		})
	}
	username, _ := doc.Get("username").(string)
	userDoc, err := s.findUserDoc(username)
	if err != nil {
		return User{}, err
	}
	if userDoc == nil {
		return User{}, ErrInvalidCredentials
	}
	return User{Username: username, Role: Role(userDoc.Get("role").(string))}, nil
}

func (s *Service) Logout(token string) error {
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		t, _ := doc.Get("token").(string)
		return t == token
	})
	return s.db.Delete(q)
}

func (s *Service) CreateUser(username, password string, role Role) error {
	if !role.Valid() {
		return ErrInvalidRole
	}
	if strings.TrimSpace(username) == "" {
		return inputError("username must not be empty")
	}
	if strings.HasPrefix(username, KeyIdentityPrefix) {
		return inputError(fmt.Sprintf("username must not start with %q (reserved for API keys)", KeyIdentityPrefix))
	}
	if len(password) < 8 {
		return errShortPassword
	}
	existing, err := s.findUserDoc(username)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrUserExists
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	doc := d.NewDocument()
	doc.Set("username", username)
	doc.Set("hash", string(hash))
	doc.Set("role", string(role))
	_, err = s.db.InsertOne(usersCollection, doc)
	return err
}

// SetPassword replaces a user's password and revokes all of their active
// sessions.
func (s *Service) SetPassword(username, password string) error {
	if len(password) < 8 {
		return errShortPassword
	}
	doc, err := s.findUserDoc(username)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.db.UpdateById(usersCollection, doc.ObjectId(), func(doc *d.Document) *d.Document {
		doc.Set("hash", string(hash))
		return doc
	}); err != nil {
		return err
	}
	return s.deleteSessionsOf(username)
}

// NewRandomPassword returns a random password (24 hex chars) for
// auto-generated accounts.
func NewRandomPassword() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ChangeOwnPassword rotates a user's password after verifying the current
// one. Every other session of the user is revoked; keepToken (the caller's
// own session) survives.
func (s *Service) ChangeOwnPassword(username, current, newPassword, keepToken string) error {
	if len(newPassword) < 8 {
		return errShortPassword
	}
	doc, err := s.findUserDoc(username)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound
	}
	hash, _ := doc.Get("hash").(string)
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.db.UpdateById(usersCollection, doc.ObjectId(), func(doc *d.Document) *d.Document {
		doc.Set("hash", string(newHash))
		return doc
	}); err != nil {
		return err
	}
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		u, _ := doc.Get("username").(string)
		t, _ := doc.Get("token").(string)
		return u == username && t != keepToken
	})
	return s.db.Delete(q)
}

// SetRole changes a user's role. The last admin cannot be demoted.
func (s *Service) SetRole(username string, role Role) error {
	if !role.Valid() {
		return ErrInvalidRole
	}
	doc, err := s.findUserDoc(username)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound
	}
	if Role(doc.Get("role").(string)) == Admin && role != Admin {
		admins, err := s.listUserDocs(Admin)
		if err != nil {
			return err
		}
		if len(admins) <= 1 {
			return ErrLastAdmin
		}
	}
	return s.db.UpdateById(usersCollection, doc.ObjectId(), func(doc *d.Document) *d.Document {
		doc.Set("role", string(role))
		return doc
	})
}

func (s *Service) DeleteUser(username string) error {
	doc, err := s.findUserDoc(username)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound
	}
	if Role(doc.Get("role").(string)) == Admin {
		admins, err := s.listUserDocs(Admin)
		if err != nil {
			return err
		}
		if len(admins) <= 1 {
			return ErrLastAdmin
		}
	}
	if err := s.db.DeleteById(usersCollection, doc.ObjectId()); err != nil {
		return err
	}
	return s.deleteSessionsOf(username)
}

func (s *Service) ListUsers() ([]User, error) {
	docs, err := s.listUserDocs("")
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(docs))
	for _, doc := range docs {
		username, _ := doc.Get("username").(string)
		role, _ := doc.Get("role").(string)
		users = append(users, User{Username: username, Role: Role(role)})
	}
	// storage order is document-id order, not creation order - sort by
	// privilege (admin first) then username so the listing is deterministic
	sort.Slice(users, func(i, j int) bool {
		pi, pj := roleRank(users[i].Role), roleRank(users[j].Role)
		if pi != pj {
			return pi < pj
		}
		return users[i].Username < users[j].Username
	})
	return users, nil
}

func roleRank(r Role) int {
	switch r {
	case Admin:
		return 0
	case Editor:
		return 1
	case Viewer:
		return 2
	case Builder:
		return 3
	}
	return 4
}

func (s *Service) findUserDoc(username string) (*d.Document, error) {
	q := query.NewQuery(usersCollection).MatchFunc(func(doc *d.Document) bool {
		u, _ := doc.Get("username").(string)
		return u == username
	})
	return s.db.FindFirst(q)
}

func (s *Service) listUserDocs(role Role) ([]*d.Document, error) {
	q := query.NewQuery(usersCollection).MatchFunc(func(doc *d.Document) bool {
		if role == "" {
			return true
		}
		r, _ := doc.Get("role").(string)
		return Role(r) == role
	})
	return s.db.FindAll(q)
}

func (s *Service) deleteSessionsOf(username string) error {
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		u, _ := doc.Get("username").(string)
		return u == username
	})
	return s.db.Delete(q)
}

func (s *Service) purgeExpiredSessions() {
	q := query.NewQuery(sessionsCollection).MatchFunc(func(doc *d.Document) bool {
		exp := parseTime(doc.Get("expiresAt"))
		return !exp.IsZero() && time.Now().After(exp)
	})
	_ = s.db.Delete(q)
}

// parseTime handles the shapes a stored time can come back as: clover
// round-trips documents through JSON, so time.Time values return as
// RFC3339 strings.
func parseTime(v interface{}) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, t)
		}
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// API keys are standalone credentials with a role: no expiry, revocable,
// never attached to a user account. Only the SHA-256 hash is stored; the
// full key is shown once at creation.
const (
	KeyPrefix      = "nri_"
	keysCollection = "_api_keys"
	// KeyIdentityPrefix marks a key's name wherever a user name would go
	// (audit entries), so a key named "admin" never reads as the admin.
	KeyIdentityPrefix = "key:"
)

type ApiKey struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Role       Role   `json:"role"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
}

func hashKey(full string) string {
	sum := sha256.Sum256([]byte(full))
	return hex.EncodeToString(sum[:])
}

// CreateAPIKey mints "nri_<32 hex>" and stores its hash. The full key is
// returned once; only prefix-identified metadata lives in the database.
// Names are unique: a key's name is its identity in the audit trail.
func (s *Service) CreateAPIKey(name string, role Role) (string, ApiKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ApiKey{}, inputError("key name must not be empty")
	}
	if !role.Valid() {
		return "", ApiKey{}, ErrInvalidRole
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	taken, err := s.db.FindFirst(query.NewQuery(keysCollection).MatchFunc(func(doc *d.Document) bool {
		return stringOr(doc.Get("name")) == name
	}))
	if err != nil {
		return "", ApiKey{}, err
	}
	if taken != nil {
		return "", ApiKey{}, ErrKeyExists
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", ApiKey{}, err
	}
	hexPart := hex.EncodeToString(buf)
	full := KeyPrefix + hexPart
	now := time.Now().UTC().Format(time.RFC3339)
	key := ApiKey{
		Name:      name,
		Prefix:    KeyPrefix + hexPart[:4],
		Role:      role,
		CreatedAt: now,
	}
	doc := d.NewDocument()
	doc.Set("name", key.Name)
	doc.Set("prefix", key.Prefix)
	doc.Set("role", string(role))
	doc.Set("hash", hashKey(full))
	doc.Set("createdAt", now)
	id, err := s.db.InsertOne(keysCollection, doc)
	if err != nil {
		return "", ApiKey{}, err
	}
	key.ID = fmt.Sprint(id)
	return full, key, nil
}

func (s *Service) ListAPIKeys() ([]ApiKey, error) {
	q := query.NewQuery(keysCollection)
	docs, err := s.db.FindAll(q)
	if err != nil {
		return nil, err
	}
	keys := make([]ApiKey, 0, len(docs))
	for _, doc := range docs {
		keys = append(keys, ApiKey{
			ID:         doc.ObjectId(),
			Name:       stringOr(doc.Get("name")),
			Prefix:     stringOr(doc.Get("prefix")),
			Role:       Role(stringOr(doc.Get("role"))),
			CreatedAt:  stringOr(doc.Get("createdAt")),
			LastUsedAt: stringOr(doc.Get("lastUsedAt")),
		})
	}
	return keys, nil
}

func (s *Service) DeleteAPIKey(id string) error {
	doc, err := s.db.FindById(keysCollection, id)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrKeyNotFound
	}
	return s.db.DeleteById(keysCollection, id)
}

// ValidateAPIKey resolves a full key to a User named "key:<name>" (how it
// shows in audit entries, never mistaken for a human account) and
// refreshes lastUsedAt at most once a minute per key.
func (s *Service) ValidateAPIKey(full string) (User, error) {
	if !strings.HasPrefix(full, KeyPrefix) {
		return User{}, ErrInvalidCredentials
	}
	hash := hashKey(full)
	q := query.NewQuery(keysCollection).MatchFunc(func(doc *d.Document) bool {
		h, _ := doc.Get("hash").(string)
		return h == hash
	})
	doc, err := s.db.FindFirst(q)
	if err != nil {
		return User{}, err
	}
	if doc == nil {
		return User{}, ErrInvalidCredentials
	}
	role := Role(stringOr(doc.Get("role")))
	if !role.Valid() {
		return User{}, ErrInvalidCredentials
	}
	if last := stringOr(doc.Get("lastUsedAt")); last == "" || staleMinute(last) {
		_ = s.db.UpdateById(keysCollection, doc.ObjectId(), func(doc *d.Document) *d.Document {
			doc.Set("lastUsedAt", time.Now().UTC().Format(time.RFC3339))
			return doc
		})
	}
	return User{Username: KeyIdentityPrefix + stringOr(doc.Get("name")), Role: role}, nil
}

func stringOr(v interface{}) string {
	s, _ := v.(string)
	return s
}

func staleMinute(rfc3339 string) bool {
	last, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return true
	}
	return time.Since(last) > time.Minute
}
