package query

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type fakeDetail struct {
	series []domain.Series
	points map[int64][]DetailPoint
}

func (f fakeDetail) ListSeries(context.Context, domain.Scope, domain.HostID) ([]domain.Series, error) {
	return f.series, nil
}
func (f fakeDetail) DetailPoints(_ context.Context, _ domain.Scope, _ domain.HostID, id int64, _, _ time.Time) ([]DetailPoint, error) {
	return f.points[id], nil
}

func TestDetailFrameGaugeMeanAndCounterRate(t *testing.T) {
	_, hosts, s := rig()
	p := func(sec int, v float64) DetailPoint {
		return DetailPoint{TS: t0.Add(time.Duration(sec) * time.Second), Value: v}
	}
	fd := fakeDetail{
		series: []domain.Series{{ID: 1, Kind: domain.KindGauge}, {ID: 2, Kind: domain.KindCounter}},
		points: map[int64][]DetailPoint{1: {p(0, 10), p(5, 30)}, 2: {p(0, 0), p(5, 100), p(25, 500)}},
	}
	d := NewDetails(hosts, fd)
	g, err := d.Frame(context.Background(), s, DetailRequest{Host: hosts.host.ID, Series: 1, From: t0, To: t0.Add(30 * time.Second), Step: 30 * time.Second})
	if err != nil || g.Values[0] != 20 {
		t.Fatalf("gauge mean = %v, %v; want 20", g.Values, err)
	}
	c, err := d.Frame(context.Background(), s, DetailRequest{Host: hosts.host.ID, Series: 2, From: t0, To: t0.Add(30 * time.Second), Step: 30 * time.Second})
	if err != nil || math.Abs(c.Values[0]-20) > 1e-9 || c.Kind != domain.KindCounter {
		t.Fatalf("counter rate = %v, %v; want 20 B/s (500 over the 25 s covered)", c.Values, err)
	}
	if _, err := d.Frame(context.Background(), s, DetailRequest{Host: hosts.host.ID, Series: 99, From: t0, To: t0.Add(time.Minute)}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown series: %v", err)
	}
}
