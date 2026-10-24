package identity

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// PasswordHasher hashes and verifies passwords. Implemented in infra
// (Argon2id); the app layer only knows encoded strings.
type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	Verify(ctx context.Context, password, encoded string) (bool, error)
}

// UserRepo stores dashboard users.
type UserRepo interface {
	CreateUser(ctx context.Context, s domain.Scope, u domain.User, passwordHash string) error
	GetUser(ctx context.Context, s domain.Scope, id domain.UserID) (domain.User, error)
	ListUsers(ctx context.Context, s domain.Scope) ([]domain.User, error)

	// FindLogin resolves an email to its user and password hash. Unscoped by
	// necessity: the login form does not know the tenant — the email finds it.
	FindLogin(ctx context.Context, email string) (domain.User, string, error)
}

// SessionRepo stores browser sessions (token hashes only).
type SessionRepo interface {
	CreateSession(ctx context.Context, s domain.Scope, sess domain.Session) error
	TouchSession(ctx context.Context, s domain.Scope, tokenHash []byte, at time.Time) error
	DeleteSession(ctx context.Context, s domain.Scope, tokenHash []byte) error

	// GetSession resolves a session token hash. Unscoped by necessity: the
	// cookie is how the tenant is found. A session whose user is disabled is
	// domain.ErrNotFound.
	GetSession(ctx context.Context, tokenHash []byte) (domain.Session, error)
}
