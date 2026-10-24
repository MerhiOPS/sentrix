package httpapi

import (
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

type seriesDTO struct {
	ID     int64             `json:"id"`
	Metric string            `json:"metric"`
	Kind   string            `json:"kind"`
	Unit   string            `json:"unit"`
	Labels map[string]string `json:"labels"`
}

type seriesListDTO struct {
	Series []seriesDTO `json:"series"`
}

type detailFrameDTO struct {
	From   time.Time  `json:"from"`
	To     time.Time  `json:"to"`
	StepS  int64      `json:"step_s"`
	Kind   string     `json:"kind"`
	TSMS   []int64    `json:"ts_ms"`
	Values []*float64 `json:"values"`
}

type processDTO struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	RSS  uint64  `json:"rss"`
}

type processSnapshotDTO struct {
	TS    time.Time    `json:"ts"`
	Procs []processDTO `json:"procs"`
}

func toSeriesList(ss []domain.Series) seriesListDTO {
	out := seriesListDTO{Series: make([]seriesDTO, 0, len(ss))}
	for _, s := range ss {
		unit := "bytes"
		if m, ok := domain.LookupMetric(s.Key.Metric); ok {
			unit = m.Unit
		}
		labels := map[string]string{}
		for k, v := range s.Labels {
			labels[k] = v
		}
		out.Series = append(out.Series, seriesDTO{ID: s.ID, Metric: s.Key.Metric, Kind: string(s.Kind), Unit: unit, Labels: labels})
	}
	return out
}

func toDetailFrame(f query.DetailFrame) detailFrameDTO {
	out := detailFrameDTO{From: f.From, To: f.To, StepS: int64(f.Step / time.Second), Kind: string(f.Kind),
		TSMS: make([]int64, len(f.TS)), Values: nullable(f.Values)}
	for i, t := range f.TS {
		out.TSMS[i] = t.UnixMilli()
	}
	return out
}

func toSnapshot(s domain.ProcessSnapshot) processSnapshotDTO {
	out := processSnapshotDTO{TS: s.TS, Procs: make([]processDTO, len(s.Procs))}
	for i, p := range s.Procs {
		out.Procs[i] = processDTO{Name: p.Name, CPU: p.CPUPercent, RSS: p.RSS}
	}
	return out
}
