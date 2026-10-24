package alerting

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// chanRepo is a ChannelRepo over a slice.
type chanRepo struct{ cs []domain.Channel }

func (r *chanRepo) ListChannels(context.Context, domain.Scope) ([]domain.Channel, error) {
	return append([]domain.Channel(nil), r.cs...), nil
}
func (r *chanRepo) CreateChannel(_ context.Context, _ domain.Scope, c domain.Channel) (int64, error) {
	c.ID = int64(len(r.cs) + 1)
	r.cs = append(r.cs, c)
	return c.ID, nil
}
func (r *chanRepo) UpdateChannel(_ context.Context, _ domain.Scope, c domain.Channel) error {
	for i := range r.cs {
		if r.cs[i].ID == c.ID {
			r.cs[i] = c
			return nil
		}
	}
	return domain.ErrNotFound
}
func (r *chanRepo) DeleteChannel(context.Context, domain.Scope, int64) error { return nil }

const secretURL = "https://hooks.slack.com/services/T000/B000/very-secret-token"

func TestChannelURLsAreMaskedAndMaskKeepsSecret(t *testing.T) {
	repo := &chanRepo{}
	a := NewAdmin(nil, repo, nil, nil)
	ctx := context.Background()
	created, err := a.CreateChannel(ctx, domain.Scope{}, domain.Channel{Name: "ops", Kind: domain.ChannelSlack, URL: secretURL})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(created.URL, "very-secret") {
		t.Fatalf("create returned the secret: %s", created.URL)
	}
	listed, _ := a.Channels(ctx, domain.Scope{})
	if len(listed) != 1 || listed[0].URL != "https://hooks.slack.com/"+domain.MaskSentinel {
		t.Fatalf("listed %+v, want the masked URL", listed)
	}
	// A rename sends the masked URL back: the stored secret must survive.
	listed[0].Name = "on-call"
	if err := a.UpdateChannel(ctx, domain.Scope{}, listed[0]); err != nil {
		t.Fatal(err)
	}
	if repo.cs[0].URL != secretURL || repo.cs[0].Name != "on-call" {
		t.Fatalf("stored %+v: the mask overwrote the secret", repo.cs[0])
	}
	if _, err := a.CreateChannel(ctx, domain.Scope{}, domain.Channel{Name: "x", Kind: domain.ChannelSlack, URL: listed[0].URL}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a masked URL was accepted on create: %v", err)
	}
	if err := a.UpdateChannel(ctx, domain.Scope{}, domain.Channel{ID: 99, Name: "x", Kind: domain.ChannelSlack, URL: listed[0].URL}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("masked update of an unknown channel: %v", err)
	}
}

func TestChannelValidation(t *testing.T) {
	a := NewAdmin(nil, &chanRepo{}, nil, nil)
	for _, u := range []string{"http://hooks.example/x", "https://user:pw@hooks.example/x", "ftp://x", "https://"} {
		if _, err := a.CreateChannel(context.Background(), domain.Scope{}, domain.Channel{Name: "x", Kind: domain.ChannelWebhook, URL: u}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q accepted: %v", u, err)
		}
	}
}
