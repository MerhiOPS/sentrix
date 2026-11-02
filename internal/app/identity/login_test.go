package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type fakeHasher struct{ verifies int }

func (f *fakeHasher) Hash(_ context.Context, p string) (string, error) { return "h:" + p, nil }
func (f *fakeHasher) Verify(_ context.Context, p, enc string) (bool, error) {
	f.verifies++
	return enc == "h:"+p, nil
}

type fakeUsers struct {
	byEmail map[string]domain.User
	hash    map[string]string
}

func (f *fakeUsers) CreateUser(context.Context, domain.Scope, domain.User, string) error { return nil }
func (f *fakeUsers) GetUser(context.Context, domain.Scope, domain.UserID) (domain.User, error) {
	return domain.User{}, domain.ErrNotFound
}
func (f *fakeUsers) ListUsers(context.Context, domain.Scope) ([]domain.User, error) { return nil, nil }
func (f *fakeUsers) FindLogin(_ context.Context, e string) (domain.User, string, error) {
	u, ok := f.byEmail[e]
	if !ok {
		return domain.User{}, "", domain.ErrNotFound
	}
	return u, f.hash[e], nil
}

type fakeSessions struct {
	m       map[string]domain.Session
	touches int
}

func (f *fakeSessions) CreateSession(_ context.Context, _ domain.Scope, s domain.Session) error {
	f.m[string(s.TokenHash)] = s
	return nil
}
func (f *fakeSessions) TouchSession(_ context.Context, _ domain.Scope, h []byte, at time.Time) error {
	f.touches++
	s := f.m[string(h)]
	s.LastSeen = at
	f.m[string(h)] = s
	return nil
}
func (f *fakeSessions) DeleteSession(_ context.Context, _ domain.Scope, h []byte) error {
	delete(f.m, string(h))
	return nil
}
func (f *fakeSessions) GetSession(_ context.Context, h []byte) (domain.Session, error) {
	s, ok := f.m[string(h)]
	if !ok {
		return domain.Session{}, domain.ErrNotFound
	}
	return s, nil
}

type loginRig struct {
	s     *Sessions
	sess  *fakeSessions
	hash  *fakeHasher
	now   *time.Time
	users *fakeUsers
}

func newLoginRig(t *testing.T) *loginRig {
	t.Helper()
	now := now0
	clock := func() time.Time { return now }
	u := domain.User{ID: domain.NewUserID(), Tenant: domain.NewTenantID(), Email: "ops@example.com", Role: domain.RoleAdmin}
	users := &fakeUsers{byEmail: map[string]domain.User{u.Email: u}, hash: map[string]string{u.Email: "h:correct horse battery"}}
	sess := &fakeSessions{m: map[string]domain.Session{}}
	h := &fakeHasher{}
	s, err := NewSessions(context.Background(), users, sess, h, NewLoginGuard(3, 6, 15*time.Minute, 100, clock), clock)
	if err != nil {
		t.Fatal(err)
	}
	return &loginRig{s: s, sess: sess, hash: h, now: &now, users: users}
}

func TestLoginSuccessIssuesFreshTokenPerLogin(t *testing.T) {
	r := newLoginRig(t)
	t1, s1, err := r.s.Login(context.Background(), " OPS@example.com ", "correct horse battery", "1.2.3.4")
	if err != nil || s1.Role != domain.RoleAdmin {
		t.Fatalf("login: %+v %v", s1, err)
	}
	t2, _, _ := r.s.Login(context.Background(), "ops@example.com", "correct horse battery", "1.2.3.4")
	if bytes.Equal(t1, t2) {
		t.Fatal("two logins returned the same token (session fixation)")
	}
	if _, ok := r.sess.m[string(t1)]; ok {
		t.Fatal("raw token stored instead of its hash")
	}
}

func TestLoginFailuresAreGenericAndCostOneVerify(t *testing.T) {
	r := newLoginRig(t)
	_, _, errWrong := r.s.Login(context.Background(), "ops@example.com", "wrong password!!", "c")
	before := r.hash.verifies
	_, _, errUnknown := r.s.Login(context.Background(), "ghost@example.com", "whatever password", "c")
	if r.hash.verifies != before+1 {
		t.Fatal("unknown email skipped password verification: timing reveals which emails exist")
	}
	if errWrong == nil || errUnknown == nil || errWrong.Error() != errUnknown.Error() {
		t.Fatalf("failures distinguishable: %v vs %v", errWrong, errUnknown)
	}
	disabled := r.users.byEmail["ops@example.com"]
	d := now0
	disabled.DisabledAt = &d
	r.users.byEmail["ops@example.com"] = disabled
	if _, _, err := r.s.Login(context.Background(), "ops@example.com", "correct horse battery", "c2"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("disabled user logged in: %v", err)
	}
}

func TestLoginThrottledAfterRepeatedFailures(t *testing.T) {
	r := newLoginRig(t)
	for i := 0; i < 3; i++ {
		_, _, _ = r.s.Login(context.Background(), "ops@example.com", "wrong password!!", "1.1.1.1")
	}
	if _, _, err := r.s.Login(context.Background(), "ops@example.com", "correct horse battery", "1.1.1.1"); !errors.Is(err, ErrLoginThrottled) {
		t.Fatalf("4th attempt: err = %v, want throttled even with the right password", err)
	}
	if _, _, err := r.s.Login(context.Background(), "ops@example.com", "correct horse battery", "2.2.2.2"); err != nil {
		t.Fatalf("another client is locked out too: %v", err)
	}
}

func TestAuthenticateExpiryAndTouchThrottle(t *testing.T) {
	r := newLoginRig(t)
	tok, _, _ := r.s.Login(context.Background(), "ops@example.com", "correct horse battery", "c")
	*r.now = now0.Add(30 * time.Second)
	if _, err := r.s.Authenticate(context.Background(), tok); err != nil || r.sess.touches != 0 {
		t.Fatalf("auth: %v, touches %d (want none within a minute)", err, r.sess.touches)
	}
	*r.now = now0.Add(2 * time.Minute)
	if _, err := r.s.Authenticate(context.Background(), tok); err != nil || r.sess.touches != 1 {
		t.Fatalf("auth after 2m: %v, touches %d (want 1)", err, r.sess.touches)
	}
	*r.now = now0.Add(2*time.Minute + domain.SessionIdle)
	if _, err := r.s.Authenticate(context.Background(), tok); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("idle session accepted: %v", err)
	}
	if len(r.sess.m) != 0 {
		t.Fatal("expired session not deleted")
	}
}

func TestLimiterFailsClosedWhenFull(t *testing.T) {
	now := now0
	l := NewLoginLimiter(3, time.Minute, 2, func() time.Time { return now })
	l.Fail("junk-1")
	l.Fail("junk-2")
	if l.Allowed("victim") {
		t.Fatal("full limiter let an untracked key through: brute force after filling the table")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allowed("victim") {
		t.Fatal("limiter still closed after entries expired")
	}
	if l.size() > 2 {
		t.Fatalf("limiter holds %d keys, max 2", l.size())
	}
}

// A brute force spread over many addresses is capped per account, and a
// throttled answer does not tell a real email from an unknown one.
func TestAccountCappedAcrossClientsWithoutRevealingIt(t *testing.T) {
	for _, email := range []string{"ops@example.com", "ghost@example.com"} {
		r := newLoginRig(t)
		for i := range 6 { // the account cap; each client stays under its own 3
			_, _, _ = r.s.Login(context.Background(), email, "wrong password!!", fmt.Sprintf("10.0.0.%d", i))
		}
		_, _, err := r.s.Login(context.Background(), email, "correct horse battery", "10.9.9.9")
		if !errors.Is(err, ErrLoginThrottled) {
			t.Fatalf("%s from a fresh client after 6 failures elsewhere: %v, want throttled", email, err)
		}
		*r.now = now0.Add(15 * time.Minute)
		_, _, err = r.s.Login(context.Background(), email, "correct horse battery", "10.9.9.9")
		if email == "ops@example.com" && err != nil {
			t.Fatalf("the account is still locked after the window: %v", err)
		}
	}
}

// Filling the tables whose keys an attacker chooses locks nobody out, and
// opens no account: the per-account cap still holds.
func TestJunkFillLocksNobodyOut(t *testing.T) {
	now := now0
	clock := func() time.Time { return now }
	users := &fakeUsers{byEmail: map[string]domain.User{"ops@example.com": {ID: domain.NewUserID(), Tenant: domain.NewTenantID(),
		Email: "ops@example.com", Role: domain.RoleAdmin}}, hash: map[string]string{"ops@example.com": "h:correct horse battery"}}
	h := &fakeHasher{}
	s, err := NewSessions(context.Background(), users, &fakeSessions{m: map[string]domain.Session{}}, h,
		NewLoginGuard(3, 6, 15*time.Minute, 10, clock), clock)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 { // more junk than either table holds
		_, _, _ = s.Login(context.Background(), fmt.Sprintf("junk%d@example.com", i), "x-wrong-password", fmt.Sprintf("10.1.0.%d", i))
	}
	if _, _, err := s.Login(context.Background(), "ops@example.com", "correct horse battery", "10.2.2.2"); err != nil {
		t.Fatalf("a real user was locked out by junk: %v", err)
	}
	for i := range 6 {
		_, _, _ = s.Login(context.Background(), "ops@example.com", "wrong password!!", fmt.Sprintf("10.3.0.%d", i))
	}
	if _, _, err := s.Login(context.Background(), "ops@example.com", "correct horse battery", "10.4.4.4"); !errors.Is(err, ErrLoginThrottled) {
		t.Fatalf("with the junk tables full the account cap must still hold: %v", err)
	}
}

func TestLimiterFailOpenWhenFull(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 2, func() time.Time { return now0 }).FailOpen()
	l.Fail("junk-1")
	l.Fail("junk-2")
	if !l.Allowed("anyone") || l.size() > 2 {
		t.Fatalf("a fail-open limiter refused an untracked key, or grew past its bound (%d)", l.size())
	}
}

func TestSuccessfulLoginClearsTheCounters(t *testing.T) {
	r := newLoginRig(t)
	for range 2 {
		for range 2 { // two typos, under the limit of 3
			_, _, _ = r.s.Login(context.Background(), "ops@example.com", "wrong password!!", "1.1.1.1")
		}
		if _, _, err := r.s.Login(context.Background(), "ops@example.com", "correct horse battery", "1.1.1.1"); err != nil {
			t.Fatalf("a login after two typos was refused: %v (a success must reset the count)", err)
		}
	}
}
