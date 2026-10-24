package livehub

import (
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func samplesAt(secs ...int) []domain.CoreSample {
	var out []domain.CoreSample
	for _, s := range secs {
		out = append(out, domain.CoreSample{TS: t0.Add(time.Duration(s) * time.Second)})
	}
	return out
}

func drain(s *Sub) []Event {
	var out []Event
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return out
			}
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestTenantIsolationInHub(t *testing.T) {
	h := New()
	a, b := domain.NewTenantID(), domain.NewTenantID()
	hostA := domain.NewHostID()
	subB := h.Subscribe(b, nil, time.Time{}) // B, every host
	h.PublishCore(a, hostA, samplesAt(0))
	if ev := drain(subB); len(ev) != 0 {
		t.Fatalf("tenant B received %d frames of tenant A's host", len(ev))
	}
	late := h.Subscribe(b, []domain.HostID{hostA}, time.Time{}) // B naming A's host id
	if ev := drain(late); len(ev) != 0 {
		t.Fatal("backfill leaked another tenant's host to a subscriber naming its id")
	}
}

func TestHostFilterAndFanout(t *testing.T) {
	h := New()
	tn := domain.NewTenantID()
	h1, h2 := domain.NewHostID(), domain.NewHostID()
	all := h.Subscribe(tn, nil, time.Now())
	only1 := h.Subscribe(tn, []domain.HostID{h1}, time.Now())
	h.PublishCore(tn, h1, samplesAt(0))
	h.PublishCore(tn, h2, samplesAt(0))
	if n := len(drain(all)); n != 2 {
		t.Fatalf("all-hosts subscriber got %d events, want 2", n)
	}
	if ev := drain(only1); len(ev) != 1 || ev[0].Host != h1 {
		t.Fatalf("host filter: %+v", ev)
	}
}

func TestBackfillSinceIsBoundedAndNewerOnly(t *testing.T) {
	h := New()
	tn := domain.NewTenantID()
	host := domain.NewHostID()
	var secs []int
	for i := 0; i < 500; i++ { // more than ringSize: oldest overwritten
		secs = append(secs, i*5)
	}
	h.PublishCore(tn, host, samplesAt(secs...))
	since := t0.Add(2400 * time.Second) // sample index 480
	ev := drain(h.Subscribe(tn, nil, since))
	if len(ev) != 1 {
		t.Fatalf("backfill events = %d", len(ev))
	}
	got := ev[0].Samples
	if len(got) != 19 || !got[0].TS.Equal(t0.Add(2405*time.Second)) || !got[18].TS.Equal(t0.Add(2495*time.Second)) {
		t.Fatalf("backfill since: %d samples, first %v", len(got), got[0].TS)
	}
	all := drain(h.Subscribe(tn, nil, time.Time{}))[0].Samples
	if len(all) != backfillMax {
		t.Fatalf("unbounded backfill: %d samples, max %d", len(all), backfillMax)
	}
	for i := 1; i < len(all); i++ {
		if !all[i].TS.After(all[i-1].TS) {
			t.Fatal("backfill not in time order")
		}
	}
}

func TestSlowSubscriberDroppedNotBlocking(t *testing.T) {
	h := New()
	tn := domain.NewTenantID()
	host := domain.NewHostID()
	slow := h.Subscribe(tn, nil, time.Now())
	done := make(chan struct{})
	go func() {
		for i := 0; i < sendBuffer+10; i++ {
			h.PublishCore(tn, host, samplesAt(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a slow subscriber")
	}
	if h.SlowDropped() != 1 || h.Subscribers() != 0 {
		t.Fatalf("slow dropped %d, subscribers %d", h.SlowDropped(), h.Subscribers())
	}
	n := 0
	for range slow.Events() { // closed after the buffered events
		n++
	}
	if n != sendBuffer {
		t.Fatalf("slow subscriber got %d events before close, want %d", n, sendBuffer)
	}
}
