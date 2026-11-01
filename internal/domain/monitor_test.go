package domain

import (
	"errors"
	"testing"
	"time"
)

func httpMonitor() Monitor {
	return Monitor{Name: "shop", Kind: MonitorHTTP, Target: "https://shop.example.com/health", Interval: time.Minute,
		Timeout: 10 * time.Second, ConfirmRetries: 1, Method: "GET", CertWarnDays: 14}
}

func TestValidateMonitor(t *testing.T) {
	tcp := Monitor{Name: "db", Kind: MonitorTCP, Target: "db.example.com:5432", Interval: time.Minute, Timeout: 5 * time.Second}
	ping := Monitor{Name: "edge", Kind: MonitorPing, Target: "203.0.113.7", Interval: time.Minute, Timeout: 5 * time.Second}
	ok := []Monitor{httpMonitor(), tcp, ping}
	with := func(base Monitor, f func(*Monitor)) Monitor { f(&base); return base }
	ok = append(ok,
		with(httpMonitor(), func(m *Monitor) { m.Target, m.CertWarnDays = "http://10.0.0.1:8080/", 0 }),
		with(httpMonitor(), func(m *Monitor) { m.Keyword, m.KeywordMode = "Welcome", KeywordPresent }),
		with(httpMonitor(), func(m *Monitor) { m.AcceptStatus = []StatusRange{{200, 200}, {401, 401}} }),
		with(httpMonitor(), func(m *Monitor) { m.Target = "https://[2001:db8::1]/" }),
		with(ping, func(m *Monitor) { m.Target = "2001:db8::1" }),
		with(ping, func(m *Monitor) { m.Target = "edge-1.example.com." }),
		with(tcp, func(m *Monitor) { m.Target = "[2001:db8::1]:22" }),
		with(httpMonitor(), func(m *Monitor) { m.Interval, m.Timeout = 24*time.Hour, 30*time.Second }),
	)
	for _, m := range ok {
		if err := ValidateMonitor(m, time.Minute); err != nil {
			t.Errorf("%s %q: %v", m.Kind, m.Target, err)
		}
	}
	bad := map[string]Monitor{
		"no name":                     with(httpMonitor(), func(m *Monitor) { m.Name = "  " }),
		"unknown kind":                with(httpMonitor(), func(m *Monitor) { m.Kind = "dns" }),
		"below the plan interval":     with(httpMonitor(), func(m *Monitor) { m.Interval = 30 * time.Second }),
		"fractional interval":         with(httpMonitor(), func(m *Monitor) { m.Interval = 90500 * time.Millisecond }),
		"timeout not below interval":  with(httpMonitor(), func(m *Monitor) { m.Interval, m.Timeout = time.Minute, time.Minute }),
		"timeout over 30s":            with(httpMonitor(), func(m *Monitor) { m.Timeout = 31 * time.Second }),
		"too many retries":            with(httpMonitor(), func(m *Monitor) { m.ConfirmRetries = 4 }),
		"ftp url":                     with(httpMonitor(), func(m *Monitor) { m.Target = "ftp://example.com/" }),
		"credentials in url":          with(httpMonitor(), func(m *Monitor) { m.Target = "https://user:pw@example.com/" }),
		"bad port":                    with(httpMonitor(), func(m *Monitor) { m.Target = "https://example.com:70000/" }),
		"bad host":                    with(httpMonitor(), func(m *Monitor) { m.Target = "https://exa mple.com/" }),
		"zoned address":               with(ping, func(m *Monitor) { m.Target = "fe80::1%eth0" }),
		"POST":                        with(httpMonitor(), func(m *Monitor) { m.Method = "POST" }),
		"keyword without mode":        with(httpMonitor(), func(m *Monitor) { m.Keyword = "x" }),
		"mode without keyword":        with(httpMonitor(), func(m *Monitor) { m.KeywordMode = KeywordAbsent }),
		"keyword with HEAD":           with(httpMonitor(), func(m *Monitor) { m.Method, m.Keyword, m.KeywordMode = "HEAD", "x", KeywordPresent }),
		"status out of range":         with(httpMonitor(), func(m *Monitor) { m.AcceptStatus = []StatusRange{{200, 600}} }),
		"reversed status range":       with(httpMonitor(), func(m *Monitor) { m.AcceptStatus = []StatusRange{{299, 200}} }),
		"cert warning on http":        with(httpMonitor(), func(m *Monitor) { m.Target = "http://example.com/" }),
		"tcp without port":            with(tcp, func(m *Monitor) { m.Target = "db.example.com" }),
		"tcp port 0":                  with(tcp, func(m *Monitor) { m.Target = "db.example.com:0" }),
		"ping with port":              with(ping, func(m *Monitor) { m.Target = "edge.example.com:80" }),
		"http options on tcp":         with(tcp, func(m *Monitor) { m.Method = "GET" }),
		"cert warning on ping":        with(ping, func(m *Monitor) { m.CertWarnDays = 7 }),
		"too many channels":           with(httpMonitor(), func(m *Monitor) { m.Channels = make([]int64, 11) }),
		"interval floor without plan": with(httpMonitor(), func(m *Monitor) { m.Interval, m.Timeout = 20*time.Second, 5*time.Second }),
	}
	for name, m := range bad {
		floor := time.Minute
		if name == "interval floor without plan" {
			floor = 0
		}
		if err := ValidateMonitor(m, floor); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err %v, want ErrInvalid", name, err)
		}
	}
}

func TestAcceptsDefaultsAndRanges(t *testing.T) {
	m := httpMonitor()
	for code, want := range map[int]bool{200: true, 301: true, 399: true, 400: false, 503: false} {
		if m.Accepts(code) != want {
			t.Errorf("default accepts %d = %v", code, !want)
		}
	}
	m.AcceptStatus = []StatusRange{{200, 204}, {401, 401}}
	for code, want := range map[int]bool{200: true, 204: true, 205: false, 301: false, 401: true} {
		if m.Accepts(code) != want {
			t.Errorf("custom accepts %d = %v", code, !want)
		}
	}
}

func TestStatusRangesRoundTrip(t *testing.T) {
	rs, err := ParseStatusRanges(" 200-299, 301 ,401")
	if err != nil || len(rs) != 3 || rs[0] != (StatusRange{200, 299}) || rs[1] != (StatusRange{301, 301}) {
		t.Fatalf("parse: %v %v", rs, err)
	}
	if s := FormatStatusRanges(rs); s != "200-299,301,401" {
		t.Fatalf("format: %q", s)
	}
	for _, s := range []string{"2xx", "200-", "-5"} {
		if _, err := ParseStatusRanges(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err %v, want ErrInvalid", s, err)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	m := httpMonitor()
	if d := m.RetryDelay(); d != 10*time.Second {
		t.Fatalf("1m interval: %s, want 10s", d)
	}
	m.Interval = 30 * time.Second
	if d := m.RetryDelay(); d != 10*time.Second {
		t.Fatalf("30s interval: %s, want 10s", d)
	}
}
