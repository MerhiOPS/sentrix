package domain

import (
	"errors"
	"testing"
	"time"
)

func TestSilenceCoverage(t *testing.T) {
	at := m0.Add(30 * time.Minute)
	rule, monitor, other := int64(1), int64(7), int64(8)
	host := NewHostID()
	win := Silence{Starts: m0, Ends: m0.Add(time.Hour)}
	with := func(f func(*Silence)) Silence { s := win; f(&s); return s }
	cases := []struct {
		name            string
		s               Silence
		alert, monitor7 bool
	}{
		{"everything", win, true, true},
		{"one monitor", with(func(s *Silence) { s.Monitor = &monitor }), false, true},
		{"another monitor", with(func(s *Silence) { s.Monitor = &other }), false, false},
		{"one rule", with(func(s *Silence) { s.Rule = &rule }), true, false},
		{"one host", with(func(s *Silence) { s.Host = &host }), true, false},
	}
	for _, c := range cases {
		if got := c.s.Covers(rule, host, at); got != c.alert {
			t.Errorf("%s: covers the alert = %v, want %v", c.name, got, c.alert)
		}
		if got := c.s.CoversMonitor(monitor, at); got != c.monitor7 {
			t.Errorf("%s: covers monitor 7 = %v, want %v", c.name, got, c.monitor7)
		}
	}
	if win.CoversMonitor(monitor, m0.Add(time.Hour)) || win.Covers(rule, host, m0.Add(-time.Second)) {
		t.Fatal("a silence covers [Starts, Ends) only")
	}
	bad := with(func(s *Silence) { s.Monitor, s.Rule = &monitor, &rule })
	if err := ValidateSilence(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a silence naming a monitor and a rule: %v, want ErrInvalid", err)
	}
}
