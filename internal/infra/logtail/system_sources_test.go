package logtail

import (
	"context"
	"strings"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// fakeRun answers commands from a script and records their arguments.
type fakeRun struct {
	replies [][]byte
	calls   [][]string
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	r := f.replies[0]
	if len(f.replies) > 1 {
		f.replies = f.replies[1:]
	}
	return r, nil
}

const journalOut = `{"__CURSOR":"s=a;i=1","__REALTIME_TIMESTAMP":"1790000000000000","PRIORITY":"3","MESSAGE":"disk failure","_SYSTEMD_UNIT":"smartd.service"}
not json at all
{"__CURSOR":"s=a;i=2","__REALTIME_TIMESTAMP":"1790000001000000","PRIORITY":"6","MESSAGE":[104,105,255],"SYSLOG_IDENTIFIER":"kernel"}
`

func TestJournaldFirstRunThenFollowsCursor(t *testing.T) {
	f := &fakeRun{replies: [][]byte{[]byte("-- cursor: s=a;i=0\n"), []byte(journalOut), []byte("")}}
	j := NewJournaldSource([]string{"smartd.service"})
	j.run = f.run
	lines, _, err := j.Poll(context.Background())
	if err != nil || len(lines) != 0 {
		t.Fatalf("first run shipped history: %d lines, %v", len(lines), err)
	}
	lines, mark, _ := j.Poll(context.Background())
	if len(lines) != 2 || mark != "s=a;i=2" {
		t.Fatalf("lines %d mark %v", len(lines), mark)
	}
	if lines[0].Level != domain.LevelError || lines[0].Source != "journald:smartd.service" || lines[0].Message != "disk failure" {
		t.Fatalf("line 0: %+v", lines[0])
	}
	if lines[1].Message != "hi�" || lines[1].Level != domain.LevelInfo || lines[1].Source != "journald:kernel" {
		t.Fatalf("byte-array MESSAGE: %+v", lines[1])
	}
	args := strings.Join(f.calls[1], " ")
	if !strings.Contains(args, "--after-cursor s=a;i=0") || !strings.Contains(args, "-u smartd.service") {
		t.Fatalf("second call: %s", args)
	}
	state, _ := j.Commit(mark)
	j2 := NewJournaldSource(nil)
	j2.run = f.run
	if err := j2.Restore(state); err != nil || j2.cursor != "s=a;i=2" {
		t.Fatalf("restore: cursor %q, %v", j2.cursor, err)
	}
}

const eventsXML = `<Events>
<Event><System><Provider Name="Service Control Manager"/><EventID>7036</EventID><Level>4</Level>
<TimeCreated SystemTime="2026-10-24T12:00:00.1234567Z"/><EventRecordID>101</EventRecordID></System>
<RenderingInfo><Message>The Print Spooler service entered the running state.</Message></RenderingInfo></Event>
<Event><System><Provider Name="disk"/><EventID>7</EventID><Level>2</Level>
<TimeCreated SystemTime="2026-10-24T12:00:01Z"/><EventRecordID>102</EventRecordID></System>
<RenderingInfo><Message>The device has a bad block.</Message></RenderingInfo></Event>
</Events>`

func TestEventLogFirstRunThenAfterRecordID(t *testing.T) {
	newest := `<Events><Event><System><EventRecordID>100</EventRecordID></System></Event></Events>`
	f := &fakeRun{replies: [][]byte{[]byte(newest), []byte(eventsXML)}}
	e := NewEventLogSource([]string{"System"})
	e.run = f.run
	if lines, _, err := e.Poll(context.Background()); err != nil || len(lines) != 0 {
		t.Fatalf("first run: %d lines, %v", len(lines), err)
	}
	lines, mark, err := e.Poll(context.Background())
	if err != nil || len(lines) != 2 {
		t.Fatalf("poll: %d lines, %v", len(lines), err)
	}
	if !strings.Contains(strings.Join(f.calls[1], " "), "EventRecordID>100") {
		t.Fatalf("query did not continue after record 100: %v", f.calls[1])
	}
	if l := lines[1]; l.Level != domain.LevelError || l.Source != "eventlog:System" || l.Labels["provider"] != "disk" || l.Labels["event_id"] != "7" {
		t.Fatalf("line 1: %+v", l)
	}
	if m := mark.(map[string]uint64); m["System"] != 102 {
		t.Fatalf("mark %v, want System=102", m)
	}
}
