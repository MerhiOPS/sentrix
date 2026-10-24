package logtail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// runner executes a command and returns its stdout. Injected for tests.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // fixed binaries, arguments built here
}

// journaldBatch bounds entries per poll.
const journaldBatch = 2000

// JournaldSource reads the systemd journal through `journalctl -o json`
// with a persisted cursor (no cgo, no libsystemd).
type JournaldSource struct {
	units     []string
	run       runner
	cursor    string // read position
	committed string
}

// NewJournaldSource follows the journal, optionally limited to units.
func NewJournaldSource(units []string) *JournaldSource {
	return &JournaldSource{units: units, run: execRunner}
}

// Name implements Source.
func (j *JournaldSource) Name() string { return "journald" }

type journalState struct {
	Cursor string `json:"cursor"`
}

// Restore implements Source.
func (j *JournaldSource) Restore(state []byte) error {
	if state == nil {
		return nil
	}
	var st journalState
	if err := json.Unmarshal(state, &st); err != nil {
		return fmt.Errorf("decode journald state: %w", err)
	}
	j.cursor, j.committed = st.Cursor, st.Cursor
	return nil
}

// Poll implements Source. With no cursor yet it only records where the
// journal ends now: a new agent does not ship the whole journal.
func (j *JournaldSource) Poll(ctx context.Context) ([]domain.LogLine, Mark, error) {
	if j.cursor == "" {
		out, err := j.run(ctx, "journalctl", "-n", "0", "--show-cursor", "--no-pager")
		if err != nil {
			return nil, j.cursor, fmt.Errorf("journalctl: %w", err)
		}
		j.cursor = parseShowCursor(out)
		return nil, j.cursor, nil
	}
	args := []string{"-o", "json", "--no-pager", "--after-cursor", j.cursor, "-n", strconv.Itoa(journaldBatch)}
	for _, u := range j.units {
		args = append(args, "-u", u)
	}
	out, err := j.run(ctx, "journalctl", args...)
	if err != nil {
		return nil, j.cursor, fmt.Errorf("journalctl: %w", err)
	}
	lines, last := parseJournal(out)
	if last != "" {
		j.cursor = last
	}
	return lines, j.cursor, nil
}

// Commit implements Source.
func (j *JournaldSource) Commit(m Mark) ([]byte, error) {
	c, ok := m.(string)
	if !ok {
		return nil, fmt.Errorf("journald: foreign mark %T", m)
	}
	j.committed = c
	return json.Marshal(journalState{Cursor: c})
}

// Close implements Source.
func (j *JournaldSource) Close() error { return nil }

func parseShowCursor(out []byte) string {
	for _, l := range strings.Split(string(out), "\n") {
		if c, ok := strings.CutPrefix(strings.TrimSpace(l), "-- cursor: "); ok {
			return c
		}
	}
	return ""
}

type journalEntry struct {
	Cursor   string          `json:"__CURSOR"`
	Realtime string          `json:"__REALTIME_TIMESTAMP"`
	Priority string          `json:"PRIORITY"`
	Message  json.RawMessage `json:"MESSAGE"`
	Unit     string          `json:"_SYSTEMD_UNIT"`
	Ident    string          `json:"SYSLOG_IDENTIFIER"`
}

// parseJournal parses `journalctl -o json` output (one object per line) and
// returns the lines and the last cursor. Unparseable entries are skipped.
func parseJournal(out []byte) ([]domain.LogLine, string) {
	var lines []domain.LogLine
	last := ""
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var e journalEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Cursor == "" {
			continue
		}
		last = e.Cursor
		us, _ := strconv.ParseInt(e.Realtime, 10, 64)
		src := e.Unit
		if src == "" {
			src = e.Ident
		}
		msg := journalMessage(e.Message)
		if len(msg) > domain.MaxMessageBytes {
			msg = strings.ToValidUTF8(msg[:domain.MaxMessageBytes], "")
		}
		lines = append(lines, domain.LogLine{TS: time.UnixMicro(us).UTC(), Source: "journald:" + src,
			Level: domain.ParseLevel(e.Priority), Message: msg})
	}
	return lines, last
}

// journalMessage decodes MESSAGE, which journald emits as a string or, for
// non-UTF-8 content, as an array of byte values.
func journalMessage(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var bs []byte
	var ints []int
	if json.Unmarshal(raw, &ints) == nil {
		for _, i := range ints {
			bs = append(bs, byte(i)) //nolint:gosec // journald byte arrays hold 0..255
		}
		return strings.ToValidUTF8(string(bs), "�")
	}
	return ""
}
