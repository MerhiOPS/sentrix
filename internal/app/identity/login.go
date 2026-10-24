package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// errBadLogin is the only failure a caller learns: unknown email, wrong
// password and disabled user are indistinguishable.
var errBadLogin = fmt.Errorf("%w: invalid email or password", domain.ErrUnauthenticated)

// ErrLoginThrottled means too many recent failures for this account+client.
var ErrLoginThrottled = fmt.Errorf("%w: too many failed logins, try again later", domain.ErrQuotaExceeded)

// sessionTouchEvery bounds session writes: LastSeen is refreshed at most
// this often, so every API request is not a database write.
const sessionTouchEvery = time.Minute

// Sessions logs users in, authenticates session tokens, and logs out.
type Sessions struct {
	users    UserRepo
	sessions SessionRepo
	hasher   PasswordHasher
	limiter  *LoginLimiter
	now      func() time.Time
	dummy    string // a real hash, verified against for unknown emails
}

// NewSessions builds the session use cases. It hashes a dummy password once
// so that a login for an unknown email costs one full verification, like a
// real one: response time must not reveal which emails exist.
func NewSessions(ctx context.Context, users UserRepo, sessions SessionRepo, hasher PasswordHasher,
	limiter *LoginLimiter, now func() time.Time) (*Sessions, error) {
	dummy, err := hasher.Hash(ctx, "dummy-password-never-matches")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy hash: %w", err)
	}
	return &Sessions{users: users, sessions: sessions, hasher: hasher, limiter: limiter, now: now, dummy: dummy}, nil
}

// Login verifies credentials and returns a new session token. A new token
// is minted on every login (no session fixation).
func (s *Sessions) Login(ctx context.Context, email, password, client string) ([]byte, domain.Session, error) {
	e, err := domain.NormalizeEmail(email)
	if err != nil {
		return nil, domain.Session{}, errBadLogin
	}
	key := e + "|" + client
	if !s.limiter.Allowed(key) {
		return nil, domain.Session{}, ErrLoginThrottled
	}
	u, hash, err := s.users.FindLogin(ctx, e)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, domain.Session{}, fmt.Errorf("find login: %w", err)
	}
	known := err == nil
	if !known {
		hash = s.dummy
	}
	ok, verr := s.hasher.Verify(ctx, password, hash)
	if verr != nil {
		return nil, domain.Session{}, fmt.Errorf("verify password: %w", verr)
	}
	if !known || !ok || u.DisabledAt != nil {
		s.limiter.Fail(key)
		return nil, domain.Session{}, errBadLogin
	}
	s.limiter.Succeed(key)
	return s.start(ctx, u)
}

func (s *Sessions) start(ctx context.Context, u domain.User) ([]byte, domain.Session, error) {
	scope, err := domain.NewScope(u.Tenant)
	if err != nil {
		return nil, domain.Session{}, err
	}
	token := NewSecret()
	now := s.now().UTC()
	sess := domain.Session{TokenHash: HashSecret(token), User: u.ID, Tenant: u.Tenant, Role: u.Role, CreatedAt: now, LastSeen: now}
	if err := s.sessions.CreateSession(ctx, scope, sess); err != nil {
		return nil, domain.Session{}, fmt.Errorf("create session: %w", err)
	}
	return token, sess, nil
}

// Authenticate resolves a session token, ending it if expired.
func (s *Sessions) Authenticate(ctx context.Context, token []byte) (domain.Session, error) {
	h := HashSecret(token)
	sess, err := s.sessions.GetSession(ctx, h)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Session{}, fmt.Errorf("%w: no session", domain.ErrUnauthenticated)
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	scope, err := domain.NewScope(sess.Tenant)
	if err != nil {
		return domain.Session{}, err
	}
	now := s.now().UTC()
	if sess.Expired(now) {
		if derr := s.sessions.DeleteSession(ctx, scope, h); derr != nil {
			return domain.Session{}, fmt.Errorf("end expired session: %w", derr)
		}
		return domain.Session{}, fmt.Errorf("%w: session expired", domain.ErrUnauthenticated)
	}
	if now.Sub(sess.LastSeen) >= sessionTouchEvery {
		if err := s.sessions.TouchSession(ctx, scope, h, now); err != nil {
			return domain.Session{}, fmt.Errorf("touch session: %w", err)
		}
		sess.LastSeen = now
	}
	return sess, nil
}

// Logout ends the session (idempotent).
func (s *Sessions) Logout(ctx context.Context, sess domain.Session) error {
	scope, err := domain.NewScope(sess.Tenant)
	if err != nil {
		return err
	}
	return s.sessions.DeleteSession(ctx, scope, sess.TokenHash)
}
