package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Users manages dashboard users.
type Users struct {
	repo   UserRepo
	hasher PasswordHasher
	now    func() time.Time
}

// NewUsers builds the user use cases.
func NewUsers(repo UserRepo, hasher PasswordHasher, now func() time.Time) *Users {
	return &Users{repo: repo, hasher: hasher, now: now}
}

// Create adds a user to the scoped tenant. The caller has already checked
// that the actor may (admin role, or the bootstrap CLI).
func (u *Users) Create(ctx context.Context, s domain.Scope, email, password string, role domain.Role) (domain.User, error) {
	e, err := domain.NormalizeEmail(email)
	if err != nil {
		return domain.User{}, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, err
	}
	if _, err := domain.ParseRole(string(role)); err != nil {
		return domain.User{}, err
	}
	tid, err := s.Tenant()
	if err != nil {
		return domain.User{}, err
	}
	hash, err := u.hasher.Hash(ctx, password)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}
	user := domain.User{ID: domain.NewUserID(), Tenant: tid, Email: e, Role: role, CreatedAt: u.now().UTC()}
	if err := u.repo.CreateUser(ctx, s, user, hash); err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}
