package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestIDRoundTripAndVersion(t *testing.T) {
	id := NewID()
	if id.IsZero() {
		t.Fatal("NewID returned zero")
	}
	s := id.String()
	if len(s) != 36 || s[14] != '4' || !strings.ContainsRune("89ab", rune(s[19])) {
		t.Fatalf("not a canonical v4 UUID: %s", s)
	}
	back, err := ParseID(s)
	if err != nil || back != id {
		t.Fatalf("round trip: %v %v", back, err)
	}
	if NewID() == id {
		t.Fatal("two NewID calls collided")
	}
}

func TestParseIDRejectsNonCanonical(t *testing.T) {
	for _, s := range []string{
		"", "not-a-uuid",
		"0190b6b4f58d72c3826a1645ab284c88",       // no hyphens
		"{0190b6b4-f58d-72c3-826a-1645ab284c88}", // braces
		"0190b6b4-f58d-72c3-826a-1645ab284c8g",   // non-hex
		"0190b6b4-f58d-72c3-826a1645ab284c88-",   // hyphen moved
	} {
		if _, err := ParseID(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseID(%q) err = %v, want ErrInvalid", s, err)
		}
	}
}

func TestScopeRequiresTenant(t *testing.T) {
	if _, err := NewScope(TenantID{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewScope(zero) err = %v, want ErrInvalid", err)
	}
	var zero Scope
	if _, err := zero.Tenant(); err == nil {
		t.Fatal("zero Scope must not yield a tenant")
	}
	tid := NewTenantID()
	s, err := NewScope(tid)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Tenant(); err != nil || got != tid {
		t.Fatalf("Tenant() = %v, %v", got, err)
	}
}

func TestOpsScopeRequiresReason(t *testing.T) {
	if _, err := NewOpsScope(""); err == nil {
		t.Fatal("empty reason accepted")
	}
	var zero OpsScope
	if _, err := zero.Reason(); err == nil {
		t.Fatal("zero OpsScope yielded a reason")
	}
	o, _ := NewOpsScope("retention janitor")
	if r, err := o.Reason(); err != nil || r != "retention janitor" {
		t.Fatalf("Reason() = %q, %v", r, err)
	}
}

func TestHostStatus(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	disabled := now
	cases := []struct {
		name string
		h    Host
		want HostStatus
	}{
		{"never seen", Host{Interval: 5 * time.Second}, StatusNoData},
		{"fresh", Host{Interval: 5 * time.Second, LastSeen: now.Add(-10 * time.Second)}, StatusOK},
		{"at 3x is still ok", Host{Interval: 5 * time.Second, LastSeen: now.Add(-15 * time.Second)}, StatusOK},
		{"past 3x", Host{Interval: 5 * time.Second, LastSeen: now.Add(-16 * time.Second)}, StatusStale},
		{"past 10x", Host{Interval: 5 * time.Second, LastSeen: now.Add(-51 * time.Second)}, StatusNoData},
		{"zero interval uses default", Host{LastSeen: now.Add(-16 * time.Second)}, StatusStale},
		{"disabled wins", Host{LastSeen: now, DisabledAt: &disabled}, StatusDisabled},
	}
	for _, c := range cases {
		if got := c.h.Status(now); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestCoreSampleValidate(t *testing.T) {
	ok := CoreSample{TS: time.Now(), CPUPercent: 12.5, MemoryPercent: 40, DiskPercent: 70, Load1: 0.5}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid sample rejected: %v", err)
	}
	bad := map[string]func(*CoreSample){
		"no ts":        func(s *CoreSample) { s.TS = time.Time{} },
		"NaN cpu":      func(s *CoreSample) { s.CPUPercent = math.NaN() },
		"Inf load":     func(s *CoreSample) { s.Load5 = math.Inf(1) },
		"cpu > 100":    func(s *CoreSample) { s.CPUPercent = 100.01 },
		"negative mem": func(s *CoreSample) { s.MemoryPercent = -1 },
	}
	for name, mutate := range bad {
		s := ok
		mutate(&s)
		if err := s.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestDetailSampleValidate(t *testing.T) {
	ok := DetailSample{TS: time.Now(), Metric: "cpu_core_percent", Kind: KindGauge, Value: 3}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]DetailSample{
		"no metric":        {TS: ok.TS, Kind: KindGauge},
		"unknown kind":     {TS: ok.TS, Metric: "m", Kind: "histogram"},
		"NaN":              {TS: ok.TS, Metric: "m", Kind: KindGauge, Value: math.NaN()},
		"negative counter": {TS: ok.TS, Metric: "m", Kind: KindCounter, Value: -1},
	} {
		if err := s.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestLabelsCanonicalIsOrderIndependentAndUnambiguous(t *testing.T) {
	a := Labels{"core": "3", "socket": "0"}
	b := Labels{"socket": "0", "core": "3"}
	if a.Canonical() != b.Canonical() || KeyFor("m", a) != KeyFor("m", b) {
		t.Fatal("label order changed the series key")
	}
	// "=" and "," inside values must not let two different sets collide.
	x := Labels{"a": "1,b=2"}
	y := Labels{"a": "1", "b": "2"}
	if KeyFor("m", x) == KeyFor("m", y) {
		t.Fatal("distinct label sets collided")
	}
	if KeyFor("m", a) == KeyFor("n", a) {
		t.Fatal("metric name ignored by key")
	}
}

func TestLabelsValidate(t *testing.T) {
	many := Labels{}
	for i := 0; i <= MaxLabels; i++ {
		many[string(rune('a'+i))] = "v"
	}
	for name, l := range map[string]Labels{
		"too many":  many,
		"empty key": {"": "v"},
		"long key":  {strings.Repeat("k", MaxLabelKeyLen+1): "v"},
		"long val":  {"k": strings.Repeat("v", MaxLabelValueLen+1)},
	} {
		if err := l.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{
		"WARNING": LevelWarn, " err ": LevelError, "3": LevelError, "notice": LevelInfo,
		"crit": LevelFatal, "trace": LevelDebug, "chatty": LevelUnknown, "": LevelUnknown,
	} {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestLogLineValidate(t *testing.T) {
	ok := LogLine{TS: time.Now(), Source: "/var/log/syslog", Level: LevelInfo, Message: "hi"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*LogLine){
		"no ts":            func(l *LogLine) { l.TS = time.Time{} },
		"huge message":     func(l *LogLine) { l.Message = strings.Repeat("x", MaxMessageBytes+1) },
		"alias not stored": func(l *LogLine) { l.Level = "warning" },
		"bad labels":       func(l *LogLine) { l.Labels = Labels{"": "x"} },
	} {
		l := ok
		mutate(&l)
		if err := l.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	sized := LogLine{Message: "abc", Source: "de", Labels: Labels{"k": "vv"}}
	if sized.Size() != 8 {
		t.Fatalf("Size() = %d, want 8", sized.Size())
	}
}
