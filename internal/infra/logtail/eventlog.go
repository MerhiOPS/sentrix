package logtail

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// eventLogBatch bounds events per channel per poll.
const eventLogBatch = 500

// EventLogSource polls Windows Event Log channels with `wevtutil`, tracking
// the last EventRecordID per channel. Security is not a default channel: it
// needs membership of Event Log Readers (PLAN Phase 4).
type EventLogSource struct {
	channels  []string
	run       runner
	read      map[string]uint64
	committed map[string]uint64
}

// NewEventLogSource follows the given channels (e.g. Application, System).
func NewEventLogSource(channels []string) *EventLogSource {
	return &EventLogSource{channels: channels, run: execRunner, read: map[string]uint64{}, committed: map[string]uint64{}}
}

// Name implements Source.
func (e *EventLogSource) Name() string { return "eventlog" }

// Restore implements Source.
func (e *EventLogSource) Restore(state []byte) error {
	if state == nil {
		return nil
	}
	m := map[string]uint64{}
	if err := json.Unmarshal(state, &m); err != nil {
		return fmt.Errorf("decode eventlog state: %w", err)
	}
	for k, v := range m {
		e.read[k], e.committed[k] = v, v
	}
	return nil
}

// Poll implements Source.
func (e *EventLogSource) Poll(ctx context.Context) ([]domain.LogLine, Mark, error) {
	var lines []domain.LogLine
	var firstErr error
	for _, ch := range e.channels {
		got, err := e.pollChannel(ctx, ch)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		lines = append(lines, got...)
	}
	mark := make(map[string]uint64, len(e.read))
	for k, v := range e.read {
		mark[k] = v
	}
	return lines, mark, firstErr
}

func (e *EventLogSource) pollChannel(ctx context.Context, ch string) ([]domain.LogLine, error) {
	last, known := e.read[ch]
	if !known { // first sight: start after the newest event, do not ship history
		out, err := e.run(ctx, "wevtutil", "qe", ch, "/c:1", "/rd:true", "/f:xml", "/e:Events")
		if err != nil {
			return nil, fmt.Errorf("wevtutil %s: %w", ch, err)
		}
		evs, _ := parseEvents(out)
		e.read[ch] = 0
		if len(evs) > 0 {
			e.read[ch] = evs[0].System.RecordID
		}
		return nil, nil
	}
	q := fmt.Sprintf("/q:*[System[(EventRecordID>%d)]]", last)
	out, err := e.run(ctx, "wevtutil", "qe", ch, q, "/c:"+strconv.Itoa(eventLogBatch), "/f:RenderedXml", "/e:Events")
	if err != nil {
		return nil, fmt.Errorf("wevtutil %s: %w", ch, err)
	}
	evs, err := parseEvents(out)
	if err != nil {
		return nil, err
	}
	lines := make([]domain.LogLine, 0, len(evs))
	for _, ev := range evs {
		lines = append(lines, ev.line(ch))
		if ev.System.RecordID > e.read[ch] {
			e.read[ch] = ev.System.RecordID
		}
	}
	return lines, nil
}

// Commit implements Source.
func (e *EventLogSource) Commit(m Mark) ([]byte, error) {
	mm, ok := m.(map[string]uint64)
	if !ok {
		return nil, fmt.Errorf("eventlog: foreign mark %T", m)
	}
	e.committed = mm
	return json.Marshal(mm)
}

// Close implements Source.
func (e *EventLogSource) Close() error { return nil }

type events struct {
	Events []event `xml:"Event"`
}

type event struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     string `xml:"EventID"`
		Level       int    `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
		RecordID uint64 `xml:"EventRecordID"`
	} `xml:"System"`
	Message string `xml:"RenderingInfo>Message"`
}

func parseEvents(out []byte) ([]event, error) {
	var evs events
	if err := xml.Unmarshal(out, &evs); err != nil {
		return nil, fmt.Errorf("parse wevtutil xml: %w", err)
	}
	return evs.Events, nil
}

// eventLevels maps Windows levels: 1 critical, 2 error, 3 warning,
// 4 information (0 = LogAlways, treated as information), 5 verbose.
var eventLevels = map[int]domain.Level{
	0: domain.LevelInfo, 1: domain.LevelFatal, 2: domain.LevelError, 3: domain.LevelWarn, 4: domain.LevelInfo, 5: domain.LevelDebug,
}

func (ev event) line(ch string) domain.LogLine {
	ts, err := time.Parse(time.RFC3339Nano, ev.System.TimeCreated.SystemTime)
	if err != nil {
		ts = time.Now()
	}
	lvl, ok := eventLevels[ev.System.Level]
	if !ok {
		lvl = domain.LevelUnknown
	}
	msg := strings.TrimSpace(ev.Message)
	if len(msg) > domain.MaxMessageBytes {
		msg = strings.ToValidUTF8(msg[:domain.MaxMessageBytes], "")
	}
	return domain.LogLine{TS: ts.UTC(), Source: "eventlog:" + ch, Level: lvl, Message: msg,
		Labels: domain.Labels{"provider": ev.System.Provider.Name, "event_id": ev.System.EventID}}
}
