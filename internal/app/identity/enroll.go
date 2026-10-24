package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// TokenTTL is how long an enrollment token can be redeemed.
const TokenTTL = time.Hour

// Host info limits: informational, but stored and displayed.
const (
	maxHostnameLen = 253
	maxInfoLen     = 100
)

// HostInfo is what an agent reports about itself at enrollment. It is never
// used for identity.
type HostInfo struct {
	Hostname     string
	OS           string
	Platform     string
	AgentVersion string
	Interval     time.Duration
}

// Enrollment issues and redeems enrollment tokens.
type Enrollment struct {
	repo EnrollmentRepo
	now  func() time.Time
}

// NewEnrollment builds the enrollment use cases.
func NewEnrollment(repo EnrollmentRepo, now func() time.Time) *Enrollment {
	return &Enrollment{repo: repo, now: now}
}

// IssueToken creates a single-use token for the scoped tenant and returns
// its secret. The caller (CLI) formats it with the server's SPKI pin.
func (e *Enrollment) IssueToken(ctx context.Context, s domain.Scope) ([]byte, time.Time, error) {
	secret := NewSecret()
	now := e.now().UTC()
	tok := EnrollmentToken{SecretHash: HashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(TokenTTL)}
	if err := e.repo.CreateToken(ctx, s, tok); err != nil {
		return nil, time.Time{}, fmt.Errorf("issue token: %w", err)
	}
	return secret, tok.ExpiresAt, nil
}

// Enroll redeems a token secret and returns the new host and the credential
// secret the agent must keep. The credential is generated here and only its
// hash is stored.
func (e *Enrollment) Enroll(ctx context.Context, tokenSecret []byte, info HostInfo) (domain.Host, []byte, error) {
	clean, err := cleanInfo(info)
	if err != nil {
		return domain.Host{}, nil, err
	}
	cred := NewSecret()
	now := e.now().UTC()
	h := domain.Host{
		ID: domain.NewHostID(), Name: clean.Hostname, OS: clean.OS, Platform: clean.Platform,
		AgentVersion: clean.AgentVersion, Interval: clean.Interval, CreatedAt: now,
	}
	host, err := e.repo.RedeemToken(ctx, HashSecret(tokenSecret), now,
		NewHost{Host: h, CredentialHash: HashSecret(cred)})
	if err != nil {
		return domain.Host{}, nil, fmt.Errorf("enroll: %w", err)
	}
	return host, cred, nil
}

// cleanInfo bounds and normalises agent-reported fields. The interval must
// be one the server can snap timestamps to.
func cleanInfo(i HostInfo) (HostInfo, error) {
	i.Hostname = strings.TrimSpace(i.Hostname)
	if i.Hostname == "" || len(i.Hostname) > maxHostnameLen {
		return i, fmt.Errorf("%w: hostname must be 1-%d characters", domain.ErrInvalid, maxHostnameLen)
	}
	switch i.OS {
	case "linux", "windows":
	default:
		return i, fmt.Errorf("%w: unsupported os %q", domain.ErrInvalid, i.OS)
	}
	for _, f := range []*string{&i.Platform, &i.AgentVersion} {
		if len(*f) > maxInfoLen {
			// Cut on bytes, then drop a rune the cut split in half.
			*f = strings.ToValidUTF8((*f)[:maxInfoLen], "")
		}
	}
	if err := domain.ValidateInterval(i.Interval); err != nil {
		return i, err
	}
	return i, nil
}
