package httpapi

import (
	"fmt"
	"net/url"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// monitorInputDTO is MonitorInput. Optional fields are pointers so an
// absent one takes its default and an explicit zero is kept.
type monitorInputDTO struct {
	Name           string  `json:"name"`
	Kind           string  `json:"kind"`
	Target         string  `json:"target"`
	IntervalS      int64   `json:"interval_s"`
	TimeoutS       *int64  `json:"timeout_s"`
	ConfirmRetries *int    `json:"confirm_retries"`
	Method         *string `json:"method"`
	AcceptStatus   *string `json:"accept_status"`
	Keyword        string  `json:"keyword"`
	KeywordMode    string  `json:"keyword_mode"`
	KeywordCase    bool    `json:"keyword_case_sensitive"`
	CertWarnDays   *int    `json:"cert_warn_days"`
	Channels       []int64 `json:"channels"`
}

// toDomain applies the documented defaults; validation is the domain's.
func (d monitorInputDTO) toDomain() (domain.Monitor, error) {
	m := domain.Monitor{Name: d.Name, Kind: domain.MonitorKind(d.Kind), Target: d.Target,
		Interval: time.Duration(d.IntervalS) * time.Second, ConfirmRetries: domain.DefaultConfirmRetries,
		Keyword: d.Keyword, KeywordMode: domain.KeywordMode(d.KeywordMode), KeywordCase: d.KeywordCase, Channels: d.Channels}
	m.Timeout = min(10*time.Second, m.Interval-time.Second)
	if d.TimeoutS != nil {
		m.Timeout = time.Duration(*d.TimeoutS) * time.Second
	}
	if d.ConfirmRetries != nil {
		m.ConfirmRetries = *d.ConfirmRetries
	}
	if m.Kind != domain.MonitorHTTP {
		if d.Method != nil || d.AcceptStatus != nil || (d.CertWarnDays != nil && *d.CertWarnDays != 0) {
			return m, fmt.Errorf("%w: method, accept_status and cert_warn_days apply to http monitors only", domain.ErrInvalid)
		}
		return m, nil
	}
	m.Method = "GET"
	if d.Method != nil {
		m.Method = *d.Method
	}
	if d.AcceptStatus != nil {
		rs, err := domain.ParseStatusRanges(*d.AcceptStatus)
		if err != nil {
			return m, err
		}
		m.AcceptStatus = rs
	}
	if u, err := url.Parse(d.Target); err == nil && u.Scheme == "https" {
		m.CertWarnDays = domain.DefaultCertWarnDays
	}
	if d.CertWarnDays != nil {
		m.CertWarnDays = *d.CertWarnDays
	}
	return m, nil
}

type monitorStateDTO struct {
	Status        string     `json:"status"`
	Since         time.Time  `json:"since"`
	LastCheck     *time.Time `json:"last_check"`
	Failures      int        `json:"failures"`
	CertStatus    *string    `json:"cert_status"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
}

type dayBarDTO struct {
	Day      string `json:"day"`
	UpS      int64  `json:"up_s"`
	DownS    int64  `json:"down_s"`
	Failures int    `json:"failures"`
}

type monitorDTO struct {
	ID           int64              `json:"id"`
	Name         string             `json:"name"`
	Kind         string             `json:"kind"`
	Target       string             `json:"target"`
	IntervalS    int64              `json:"interval_s"`
	TimeoutS     int64              `json:"timeout_s"`
	Retries      int                `json:"confirm_retries"`
	Method       string             `json:"method"`
	AcceptStatus string             `json:"accept_status"`
	Keyword      string             `json:"keyword"`
	KeywordMode  string             `json:"keyword_mode"`
	KeywordCase  bool               `json:"keyword_case_sensitive"`
	CertWarnDays int                `json:"cert_warn_days"`
	Channels     []int64            `json:"channels"`
	Paused       bool               `json:"paused"`
	CreatedAt    time.Time          `json:"created_at"`
	State        monitorStateDTO    `json:"state"`
	Uptime       map[string]float64 `json:"uptime"`
	AvgLatencyMS *int64             `json:"avg_latency_ms"`
	Bars         []dayBarDTO        `json:"bars"`
}

type monitorListDTO struct {
	Monitors      []monitorDTO `json:"monitors"`
	PingAvailable bool         `json:"ping_available"`
}

var windowNames = map[time.Duration]string{uptime.Window24h: "24h", uptime.Window7d: "7d", uptime.Window30d: "30d", uptime.Window90d: "90d"}

func toMonitor(v uptime.View) monitorDTO {
	m, st := v.Monitor, v.State
	out := monitorDTO{ID: m.ID, Name: m.Name, Kind: string(m.Kind), Target: m.Target, IntervalS: int64(m.Interval / time.Second),
		TimeoutS: int64(m.Timeout / time.Second), Retries: m.ConfirmRetries, Method: m.Method,
		AcceptStatus: domain.FormatStatusRanges(m.AcceptStatus), Keyword: m.Keyword, KeywordMode: string(m.KeywordMode),
		KeywordCase: m.KeywordCase, CertWarnDays: m.CertWarnDays, Channels: append(make([]int64, 0, len(m.Channels)), m.Channels...),
		Paused: m.Paused, CreatedAt: m.CreatedAt, Uptime: map[string]float64{}, Bars: make([]dayBarDTO, 0, len(v.Bars)),
		State: monitorStateDTO{Status: string(st.Status), Since: st.Since, Failures: st.Failures, CertExpiresAt: st.CertExpiry}}
	if m.Kind == domain.MonitorHTTP && out.AcceptStatus == "" {
		out.AcceptStatus = domain.FormatStatusRanges(domain.DefaultAcceptStatus)
	}
	if out.State.Status == "" {
		out.State.Status = string(domain.MonitorPending)
	}
	if !st.LastCheck.IsZero() {
		last := st.LastCheck
		out.State.LastCheck = &last
	}
	if st.Cert != domain.CertUnknown {
		c := string(st.Cert)
		out.State.CertStatus = &c
	}
	for w, ratio := range v.Uptime {
		out.Uptime[windowNames[w]] = ratio
	}
	if v.AvgLatency != nil {
		ms := v.AvgLatency.Milliseconds()
		out.AvgLatencyMS = &ms
	}
	for _, b := range v.Bars {
		out.Bars = append(out.Bars, dayBarDTO{Day: b.Day.Format(time.DateOnly), UpS: int64(b.Up / time.Second),
			DownS: int64(b.Down / time.Second), Failures: b.Failures})
	}
	return out
}

type checkResultDTO struct {
	At            time.Time  `json:"at"`
	Outcome       string     `json:"outcome"`
	Error         *string    `json:"error"`
	LatencyMS     *int64     `json:"latency_ms"`
	TTFBMS        *int64     `json:"ttfb_ms"`
	Status        *int       `json:"status"`
	Loss          *int       `json:"loss"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
}

func toCheckResult(r domain.CheckResult) checkResultDTO {
	out := checkResultDTO{At: r.At, Outcome: string(r.Outcome), CertExpiresAt: r.CertExpiry, Error: optClass(r.Class)}
	if r.Outcome != domain.CheckUnknown && (r.Latency > 0 || r.Outcome == domain.CheckUp) {
		ms := r.Latency.Milliseconds()
		out.LatencyMS = &ms
	}
	if r.TTFB > 0 {
		ms := r.TTFB.Milliseconds()
		out.TTFBMS = &ms
	}
	if r.StatusCode > 0 {
		out.Status = &r.StatusCode
	}
	if r.Loss > 0 || (r.Outcome == domain.CheckDown && r.Class == domain.ErrPingLoss) {
		out.Loss = &r.Loss
	}
	return out
}

func optClass(c domain.ErrorClass) *string {
	if c == domain.ErrNone {
		return nil
	}
	s := string(c)
	return &s
}

type incidentDTO struct {
	ID        int64      `json:"id"`
	Started   time.Time  `json:"started"`
	Ended     *time.Time `json:"ended"`
	Error     string     `json:"error"`
	Failures  int        `json:"failures"`
	DurationS int64      `json:"duration_s"`
}

type monitorEventDTO struct {
	ID            int64      `json:"id"`
	Kind          string     `json:"kind"`
	At            time.Time  `json:"at"`
	Error         *string    `json:"error"`
	DownForS      *int64     `json:"down_for_s"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
	Notified      bool       `json:"notified"`
	Outcome       string     `json:"outcome"`
	Delivery      string     `json:"delivery"`
}

func toIncidents(inc []domain.Incident, evs []domain.MonitorEvent, now time.Time) incidentListDTO {
	out := incidentListDTO{Incidents: make([]incidentDTO, 0, len(inc)), Events: make([]monitorEventDTO, 0, len(evs))}
	for _, in := range inc {
		end := now
		if in.End != nil {
			end = *in.End
		}
		out.Incidents = append(out.Incidents, incidentDTO{ID: in.ID, Started: in.Start, Ended: in.End, Error: string(in.Class),
			Failures: in.Failures, DurationS: int64(end.Sub(in.Start) / time.Second)})
	}
	for _, ev := range evs {
		d := monitorEventDTO{ID: ev.ID, Kind: string(ev.Kind), At: ev.At, Error: optClass(ev.Class), CertExpiresAt: ev.CertExpiry,
			Notified: ev.Notify, Outcome: ev.Outcome, Delivery: ev.Delivery}
		if ev.Kind == domain.EventUp {
			s := int64(ev.DownFor / time.Second)
			d.DownForS = &s
		}
		out.Events = append(out.Events, d)
	}
	return out
}

type incidentListDTO struct {
	Incidents []incidentDTO     `json:"incidents"`
	Events    []monitorEventDTO `json:"events"`
}
