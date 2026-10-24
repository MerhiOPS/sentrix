package httpapi

import (
	"math"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// DTOs mirror docs/openapi.yaml. Domain types never carry JSON tags; this
// file is the only place they are mapped.

type meDTO struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	TenantID string `json:"tenant_id"`
}

type sampleDTO struct {
	TSMS    int64   `json:"ts_ms"`
	CPU     float64 `json:"cpu"`
	Mem     float64 `json:"mem"`
	Disk    float64 `json:"disk"`
	Load1   float64 `json:"load1"`
	Load5   float64 `json:"load5"`
	Load15  float64 `json:"load15"`
	MemUsed uint64  `json:"mem_used"`
	Uptime  uint64  `json:"uptime"`
	NetRx   uint64  `json:"net_rx"`
	NetTx   uint64  `json:"net_tx"`
	DiskR   uint64  `json:"disk_r"`
	DiskW   uint64  `json:"disk_w"`
}

type healthDTO struct {
	BufferDepth   int    `json:"buffer_depth"`
	DroppedTotal  uint64 `json:"dropped_total"`
	ClockSkewMS   int64  `json:"clock_skew_ms"`
	RejectedTotal uint64 `json:"rejected_total"`
	ConflictTotal uint64 `json:"conflict_total"`
}

type hostDTO struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	OS           string     `json:"os"`
	Platform     string     `json:"platform"`
	AgentVersion string     `json:"agent_version"`
	IntervalMS   int64      `json:"interval_ms"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeen     *time.Time `json:"last_seen"`
	Health       healthDTO  `json:"health"`
	Latest       *sampleDTO `json:"latest"`
}

type hostListDTO struct {
	Hosts      []hostDTO `json:"hosts"`
	ServerTime time.Time `json:"server_time"`
}

type frameDTO struct {
	From   time.Time             `json:"from"`
	To     time.Time             `json:"to"`
	StepS  int64                 `json:"step_s"`
	TSMS   []int64               `json:"ts_ms"`
	Series map[string][]*float64 `json:"series"`
}

type tokenDTO struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// coreFrameDTO is the WebSocket message.
type coreFrameDTO struct {
	Type    string      `json:"type"`
	HostID  string      `json:"host_id"`
	Samples []sampleDTO `json:"samples"`
}

func toMe(u domain.User) meDTO {
	return meDTO{UserID: u.ID.String(), Email: u.Email, Role: string(u.Role), TenantID: u.Tenant.String()}
}

func toSample(s domain.CoreSample) sampleDTO {
	return sampleDTO{
		TSMS: s.TS.UnixMilli(), CPU: s.CPUPercent, Mem: s.MemoryPercent, Disk: s.DiskPercent,
		Load1: s.Load1, Load5: s.Load5, Load15: s.Load15, MemUsed: s.MemoryUsed, Uptime: s.UptimeSeconds,
		NetRx: s.NetRxBytes, NetTx: s.NetTxBytes, DiskR: s.DiskReadBytes, DiskW: s.DiskWriteBytes,
	}
}

func toHost(h domain.Host, latest *domain.CoreSample, now time.Time) hostDTO {
	d := hostDTO{
		ID: h.ID.String(), Name: h.Name, OS: h.OS, Platform: h.Platform, AgentVersion: h.AgentVersion,
		IntervalMS: h.Interval.Milliseconds(), Status: string(h.Status(now)), CreatedAt: h.CreatedAt,
		Health: healthDTO{
			BufferDepth: h.Health.BufferDepth, DroppedTotal: h.Health.DroppedTotal,
			ClockSkewMS: h.Health.ClockSkew.Milliseconds(), RejectedTotal: h.Health.RejectedTotal,
			ConflictTotal: h.Health.ConflictTotal,
		},
	}
	if !h.LastSeen.IsZero() {
		ls := h.LastSeen
		d.LastSeen = &ls
	}
	if latest != nil {
		s := toSample(*latest)
		d.Latest = &s
	}
	return d
}

// toFrame renders NaN (no data) as JSON null, so charts draw a gap.
func toFrame(f query.Frame) frameDTO {
	out := frameDTO{From: f.From, To: f.To, StepS: int64(f.Step / time.Second),
		TSMS: make([]int64, len(f.TS)), Series: map[string][]*float64{}}
	for i, t := range f.TS {
		out.TSMS[i] = t.UnixMilli()
	}
	for g, col := range f.Series {
		vals := make([]*float64, len(col))
		for i, v := range col {
			if !math.IsNaN(v) {
				vals[i] = &col[i]
			}
		}
		out.Series[string(g)] = vals
	}
	return out
}
