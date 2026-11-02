package identity

import (
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// LoginGuard throttles failed logins three ways:
//
//   - per account and client: the tight limit a single client hits first;
//   - per known account, across every client: a brute force spread over
//     many addresses is still capped. Keyed by user id, so its table only
//     ever holds real accounts and cannot be filled with junk; it fails
//     closed;
//   - per unknown email, across every client, with the same limit, so a
//     throttled answer does not reveal whether an email exists.
//
// The first and last tables take attacker-chosen keys and could be filled
// with junk. They fail open: the per-account table still bounds every real
// account, so filling them locks nobody out and opens no account.
type LoginGuard struct {
	pairs, accounts, unknown *LoginLimiter
}

// NewLoginGuard allows perClient failures per (email, client) and
// perAccount per account, both per window, tracking at most maxKeys keys
// in each table.
func NewLoginGuard(perClient, perAccount int, window time.Duration, maxKeys int, now func() time.Time) *LoginGuard {
	return &LoginGuard{
		pairs:    NewLoginLimiter(perClient, window, maxKeys, now).FailOpen(),
		accounts: NewLoginLimiter(perAccount, window, maxKeys, now),
		unknown:  NewLoginLimiter(perAccount, window, maxKeys, now).FailOpen(),
	}
}

// subject is who a login attempt is for: a known user, or an email that
// matches no user.
type subject struct {
	email string
	user  *domain.UserID
}

func (g *LoginGuard) account(s subject) (*LoginLimiter, string) {
	if s.user != nil {
		return g.accounts, s.user.String()
	}
	return g.unknown, s.email
}

func (g *LoginGuard) allowed(s subject, client string) bool {
	l, key := g.account(s)
	return g.pairs.Allowed(s.email+"|"+client) && l.Allowed(key)
}

func (g *LoginGuard) fail(s subject, client string) {
	l, key := g.account(s)
	g.pairs.Fail(s.email + "|" + client)
	l.Fail(key)
}

func (g *LoginGuard) succeed(s subject, client string) {
	l, key := g.account(s)
	g.pairs.Succeed(s.email + "|" + client)
	l.Succeed(key)
}
