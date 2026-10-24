// Package query is the read side: hosts and metrics for the dashboard API.
package query

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// HostReader reads hosts within a scope. Another tenant's host is
// domain.ErrNotFound, never forbidden (PLAN §4.1: existence is not leaked).
type HostReader interface {
	GetHost(ctx context.Context, s domain.Scope, id domain.HostID) (domain.Host, error)
	ListHosts(ctx context.Context, s domain.Scope) ([]domain.Host, error)
}

// CoreReader reads raw core samples. Counters come back reset-adjusted.
// The range is [from, to), ordered by time.
type CoreReader interface {
	RawCore(ctx context.Context, s domain.Scope, host domain.HostID, from, to time.Time) ([]domain.CoreSample, error)
}
