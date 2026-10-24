package httpapi

import (
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

type logEntryDTO struct {
	ID      int64             `json:"id"`
	HostID  string            `json:"host_id"`
	TS      time.Time         `json:"ts"`
	Source  string            `json:"source"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

type logPageDTO struct {
	Entries    []logEntryDTO `json:"entries"`
	NextCursor *string       `json:"next_cursor"`
}

type liveLineDTO struct {
	TS      time.Time         `json:"ts"`
	Source  string            `json:"source"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

type logFrameDTO struct {
	Type   string        `json:"type"`
	HostID string        `json:"host_id"`
	Lines  []liveLineDTO `json:"lines"`
}

type ruleDTO struct {
	ID          int64  `json:"id"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Action      string `json:"action"`
}

type ruleListDTO struct {
	Rules []ruleDTO `json:"rules"`
}

func labelsOf(l domain.Labels) map[string]string {
	out := map[string]string{}
	for k, v := range l {
		out[k] = v
	}
	return out
}

func toLogPage(p query.LogPage) logPageDTO {
	out := logPageDTO{Entries: make([]logEntryDTO, 0, len(p.Entries))}
	for _, e := range p.Entries {
		out.Entries = append(out.Entries, logEntryDTO{ID: e.ID, HostID: e.Host.String(), TS: e.Line.TS,
			Source: e.Line.Source, Level: string(e.Line.Level), Message: e.Line.Message, Labels: labelsOf(e.Line.Labels)})
	}
	if p.Next != "" {
		n := p.Next
		out.NextCursor = &n
	}
	return out
}

func toLiveLines(ls []domain.LogLine) []liveLineDTO {
	out := make([]liveLineDTO, len(ls))
	for i, l := range ls {
		out[i] = liveLineDTO{TS: l.TS, Source: l.Source, Level: string(l.Level), Message: l.Message, Labels: labelsOf(l.Labels)}
	}
	return out
}

func toRule(r domain.RedactionRule) ruleDTO {
	return ruleDTO{ID: r.ID, Pattern: r.Pattern, Replacement: r.Replacement, Action: string(r.Action)}
}
