package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var now0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type fakeEnroll struct {
	tokens   []EnrollmentToken
	redeemed NewHost
	err      error
}

func (f *fakeEnroll) CreateToken(_ context.Context, _ domain.Scope, t EnrollmentToken) error {
	f.tokens = append(f.tokens, t)
	return nil
}

func (f *fakeEnroll) RedeemToken(_ context.Context, h []byte, _ time.Time, nh NewHost) (domain.Host, error) {
	if f.err != nil {
		return domain.Host{}, f.err
	}
	for _, t := range f.tokens {
		if bytes.Equal(t.SecretHash, h) {
			f.redeemed = nh
			return nh.Host, nil
		}
	}
	return domain.Host{}, domain.ErrNotFound
}

func scope(t *testing.T) domain.Scope {
	t.Helper()
	s, err := domain.NewScope(domain.NewTenantID())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func info() HostInfo {
	return HostInfo{Hostname: "web-1", OS: "linux", Platform: "ubuntu 24.04", AgentVersion: "0.1.0", Interval: 5 * time.Second}
}

func TestIssueTokenStoresOnlyTheHash(t *testing.T) {
	repo := &fakeEnroll{}
	e := NewEnrollment(repo, func() time.Time { return now0 })
	secret, exp, err := e.IssueToken(context.Background(), scope(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != SecretLen || !exp.Equal(now0.Add(TokenTTL)) {
		t.Fatalf("secret len %d, expires %v", len(secret), exp)
	}
	if bytes.Equal(repo.tokens[0].SecretHash, secret) || !bytes.Equal(repo.tokens[0].SecretHash, HashSecret(secret)) {
		t.Fatal("stored value is not the hash of the secret")
	}
}

func TestEnrollStoresCredentialHashAndReturnsSecret(t *testing.T) {
	repo := &fakeEnroll{}
	e := NewEnrollment(repo, func() time.Time { return now0 })
	secret, _, _ := e.IssueToken(context.Background(), scope(t))
	host, cred, err := e.Enroll(context.Background(), secret, info())
	if err != nil {
		t.Fatal(err)
	}
	if host.Name != "web-1" || host.ID.IsZero() {
		t.Fatalf("host = %+v", host)
	}
	if !bytes.Equal(repo.redeemed.CredentialHash, HashSecret(cred)) || bytes.Equal(repo.redeemed.CredentialHash, cred) {
		t.Fatal("credential not stored as its hash")
	}
	if _, _, err := e.Enroll(context.Background(), NewSecret(), info()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown token: err = %v", err)
	}
}

func TestEnrollValidatesHostInfo(t *testing.T) {
	e := NewEnrollment(&fakeEnroll{}, func() time.Time { return now0 })
	for name, mutate := range map[string]func(*HostInfo){
		"empty hostname": func(i *HostInfo) { i.Hostname = "  " },
		"bad os":         func(i *HostInfo) { i.OS = "plan9" },
		"bad interval":   func(i *HostInfo) { i.Interval = 1500 * time.Millisecond },
		"zero interval":  func(i *HostInfo) { i.Interval = 0 },
	} {
		i := info()
		mutate(&i)
		if _, _, err := e.Enroll(context.Background(), NewSecret(), i); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestCleanInfoTruncatesOnRuneBoundary(t *testing.T) {
	i := info()
	i.Platform = strings.Repeat("a", maxInfoLen-1) + "é" // é is 2 bytes: cut lands inside it
	out, err := cleanInfo(i)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Platform, "aaa") || strings.ContainsRune(out.Platform, '�') || len(out.Platform) > maxInfoLen {
		t.Fatalf("bad truncation: %q (%d bytes)", out.Platform, len(out.Platform))
	}
}

type fakeTenants struct{ created []domain.Tenant }

func (f *fakeTenants) CreateTenant(_ context.Context, ops domain.OpsScope, t domain.Tenant) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	f.created = append(f.created, t)
	return nil
}
func (f *fakeTenants) GetTenant(context.Context, domain.Scope) (domain.Tenant, error) {
	return domain.Tenant{}, domain.ErrNotFound
}
func (f *fakeTenants) SetSeriesLimit(context.Context, domain.OpsScope, domain.TenantID, int) error {
	return nil
}
func (f *fakeTenants) SetLogLimit(context.Context, domain.OpsScope, domain.TenantID, int64) error {
	return nil
}

func TestCreateTenant(t *testing.T) {
	repo := &fakeTenants{}
	tn, err := NewTenants(repo, func() time.Time { return now0 }).Create(context.Background(), "  Acme  ")
	if err != nil || tn.Name != "Acme" || tn.ID.IsZero() || len(repo.created) != 1 {
		t.Fatalf("tenant=%+v err=%v", tn, err)
	}
	if _, err := NewTenants(repo, time.Now).Create(context.Background(), ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty name: err = %v", err)
	}
}
