package memstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type user struct {
	u    domain.User
	hash string
}

type session struct {
	user      domain.UserID
	tenant    domain.TenantID
	createdAt time.Time
	lastSeen  time.Time
}

// CreateUser implements identity.UserRepo.
func (s *Store) CreateUser(_ context.Context, sc domain.Scope, u domain.User, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	for _, x := range s.users {
		if x.u.Email == u.Email || x.u.ID == u.ID {
			return fmt.Errorf("user %s: %w", u.Email, domain.ErrConflict)
		}
	}
	u.Tenant = tid
	s.users[u.ID] = &user{u: u, hash: passwordHash}
	return nil
}

// GetUser implements identity.UserRepo.
func (s *Store) GetUser(_ context.Context, sc domain.Scope, id domain.UserID) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return domain.User{}, err
	}
	x, ok := s.users[id]
	if !ok || x.u.Tenant != tid {
		return domain.User{}, fmt.Errorf("user: %w", domain.ErrNotFound)
	}
	return x.u, nil
}

// ListUsers implements identity.UserRepo, ordered by email.
func (s *Store) ListUsers(_ context.Context, sc domain.Scope) ([]domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.User
	for _, x := range s.users {
		if x.u.Tenant == tid {
			out = append(out, x.u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// FindLogin implements identity.UserRepo.
func (s *Store) FindLogin(_ context.Context, email string) (domain.User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.users {
		if x.u.Email == email {
			return x.u, x.hash, nil
		}
	}
	return domain.User{}, "", fmt.Errorf("login: %w", domain.ErrNotFound)
}

// CreateSession implements identity.SessionRepo.
func (s *Store) CreateSession(_ context.Context, sc domain.Scope, sess domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if x, ok := s.users[sess.User]; !ok || x.u.Tenant != tid {
		return fmt.Errorf("session user: %w", domain.ErrNotFound)
	}
	s.sessions[string(sess.TokenHash)] = &session{user: sess.User, tenant: tid, createdAt: sess.CreatedAt, lastSeen: sess.LastSeen}
	return nil
}

// GetSession implements identity.SessionRepo: role read live from the user.
func (s *Store) GetSession(_ context.Context, tokenHash []byte) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.sessions[string(tokenHash)]
	if !ok {
		return domain.Session{}, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	u, ok := s.users[x.user]
	if !ok || u.u.DisabledAt != nil {
		return domain.Session{}, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	return domain.Session{
		TokenHash: append([]byte(nil), tokenHash...), User: x.user, Tenant: x.tenant,
		Role: u.u.Role, CreatedAt: x.createdAt, LastSeen: x.lastSeen,
	}, nil
}

// TouchSession implements identity.SessionRepo.
func (s *Store) TouchSession(_ context.Context, sc domain.Scope, tokenHash []byte, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if x, ok := s.sessions[string(tokenHash)]; ok && x.tenant == tid {
		x.lastSeen = at
	}
	return nil
}

// DeleteSession implements identity.SessionRepo (idempotent).
func (s *Store) DeleteSession(_ context.Context, sc domain.Scope, tokenHash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if x, ok := s.sessions[string(tokenHash)]; ok && x.tenant == tid {
		delete(s.sessions, string(tokenHash))
	}
	return nil
}
