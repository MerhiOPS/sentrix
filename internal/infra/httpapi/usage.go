package httpapi

import (
	"context"
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// PlanReader reads a tenant's plan and usage.
type PlanReader interface {
	Get(ctx context.Context, s domain.Scope) (domain.Plan, domain.Usage, error)
}

type usageDTO struct {
	Plan struct {
		MaxHosts             int   `json:"max_hosts"`
		LogBytesPerDay       int64 `json:"log_bytes_per_day"`
		MetricsRetentionDays int   `json:"metrics_retention_days"`
		LogsRetentionDays    int   `json:"logs_retention_days"`
	} `json:"plan"`
	Usage struct {
		Hosts          int   `json:"hosts"`
		LogBytesToday  int64 `json:"log_bytes_today"`
		MonthSamples   int64 `json:"month_samples"`
		MonthLogBytes  int64 `json:"month_log_bytes"`
		MonthPeakHosts int   `json:"month_peak_hosts"`
	} `json:"usage"`
}

// handleUsage: every role sees its limits before hitting them.
func (a *API) handleUsage(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, u, err := a.cfg.Plans.Get(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out usageDTO
	out.Plan.MaxHosts, out.Plan.LogBytesPerDay = p.MaxHosts, p.LogBytesPerDay
	out.Plan.MetricsRetentionDays, out.Plan.LogsRetentionDays = p.MetricsRetentionDays, p.LogsRetentionDays
	out.Usage.Hosts, out.Usage.LogBytesToday = u.Hosts, u.LogBytesToday
	out.Usage.MonthSamples, out.Usage.MonthLogBytes, out.Usage.MonthPeakHosts = u.MonthSamples, u.MonthLogBytes, u.MonthPeakHosts
	writeJSON(w, http.StatusOK, out)
}
