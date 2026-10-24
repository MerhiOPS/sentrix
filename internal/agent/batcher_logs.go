package agent

import (
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// Per-batch log limits, well inside the server's caps (5000 lines, 8 MiB
// decompressed): a burst splits into several batches instead of one the
// server would refuse as too large.
const (
	maxLogsPerBatch     = 2000
	maxLogBytesPerBatch = 3 << 20
)

// AddLogs records one poll of a log source and the mark after it.
func (b *Batcher) AddLogs(source string, lines []domain.LogLine, mark logtail.Mark) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range lines {
		b.logs = append(b.logs, wire.LogLine{TSMS: l.TS.UnixMilli(), Source: l.Source, Level: string(l.Level),
			Message: l.Message, Labels: l.Labels})
	}
	if b.marks == nil {
		b.marks = map[string]logtail.Mark{}
	}
	b.marks[source] = mark // newer marks supersede older: positions only move forward
}

// contents is one seal's worth of data.
type contents struct {
	samples []wire.Sample
	details []wire.Detail
	snaps   []wire.Snapshot
	logs    []wire.LogLine
	marks   map[string]logtail.Mark
}

func (c contents) empty() bool {
	return len(c.samples) == 0 && len(c.details) == 0 && len(c.snaps) == 0 && len(c.logs) == 0 && len(c.marks) == 0
}

// split cuts logs into batch-sized chunks. Metrics ride in the first chunk;
// the marks ride in the last, so a position is committed only once every
// line before it has been acknowledged.
func (c contents) split() []contents {
	var chunks [][]wire.LogLine
	start, bytes := 0, 0
	for i, l := range c.logs {
		sz := len(l.Message) + len(l.Source) + 64
		if i > start && (i-start >= maxLogsPerBatch || bytes+sz > maxLogBytesPerBatch) {
			chunks = append(chunks, c.logs[start:i])
			start, bytes = i, 0
		}
		bytes += sz
	}
	if start < len(c.logs) || len(chunks) == 0 {
		chunks = append(chunks, c.logs[start:])
	}
	out := make([]contents, len(chunks))
	for i, ch := range chunks {
		out[i].logs = ch
	}
	out[0].samples, out[0].details, out[0].snaps = c.samples, c.details, c.snaps
	out[len(out)-1].marks = c.marks
	return out
}
