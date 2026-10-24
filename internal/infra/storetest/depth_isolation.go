package storetest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// seedSeries gives f one detail series and returns its id.
func seedSeries(ctx context.Context, st Stores, f fixture, seq uint64) (int64, error) {
	d := detail(0, domain.MetricCPUCore, domain.KindGauge, domain.Labels{"core": "7"}, 50)
	if _, err := st.CommitBatch(ctx, f.scope, f.host, seq, withDetails(ingest.DefaultWindow.SeqRetention(), d)); err != nil {
		return 0, err
	}
	ss, err := st.ListSeries(ctx, f.scope, f.host)
	if err != nil {
		return 0, fmt.Errorf("seed series: %w", err)
	}
	if len(ss) == 0 {
		return 0, errors.New("seed series: none listed after commit")
	}
	return ss[0].ID, nil
}

// depthCrossCases extend crossCases for the Phase 3 read ports.
var depthCrossCases = map[string]crossCase{
	"RollupReader.Rollups": func(ctx context.Context, st Stores, a, b fixture) string {
		got, err := st.Rollups(ctx, b.scope, a.host, query.Res1m, T0.Add(-time.Hour), T0.Add(time.Hour))
		if len(got) > 0 || !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("Rollups(B, A's host) = %d buckets, %v; want ErrNotFound", len(got), err)
		}
		return ""
	},
	"DetailReader.ListSeries": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := seedSeries(ctx, st, a, 501); err != nil {
			return err.Error()
		}
		got, err := st.ListSeries(ctx, b.scope, a.host)
		if len(got) > 0 || !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("ListSeries(B, A's host) = %d series, %v; want ErrNotFound", len(got), err)
		}
		return ""
	},
	"DetailReader.DetailPoints": func(ctx context.Context, st Stores, a, b fixture) string {
		sid, err := seedSeries(ctx, st, a, 502)
		if err != nil {
			return err.Error()
		}
		// B with A's host, and — the subtle one — B with B's own host but
		// A's series id: the series must belong to the host *and* tenant.
		for _, host := range []domain.HostID{a.host, b.host} {
			pts, err := st.DetailPoints(ctx, b.scope, host, sid, T0.Add(-time.Hour), T0.Add(time.Hour))
			if len(pts) > 0 || !errors.Is(err, domain.ErrNotFound) {
				return fmt.Sprintf("DetailPoints(B, host %v, A's series) = %d points, %v; want ErrNotFound", host, len(pts), err)
			}
		}
		return ""
	},
	"ProcessReader.ProcessesAt": func(ctx context.Context, st Stores, a, b fixture) string {
		_, err := st.CommitBatch(ctx, a.scope, a.host, 503, func(s ingest.TxState) (ingest.Prepared, error) {
			return ingest.Prepared{Processes: []domain.ProcessSnapshot{{TS: T0, Procs: []domain.Process{{Name: "secret-daemon"}}}},
				Counters: s.Counters, Interval: 5 * time.Second, ReceivedAt: T0, SeqRetention: ingest.DefaultWindow.SeqRetention()}, nil
		})
		if err != nil {
			return err.Error()
		}
		got, err := st.ProcessesAt(ctx, b.scope, a.host, T0.Add(time.Hour))
		if len(got.Procs) > 0 || !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("ProcessesAt(B, A's host) = %+v, %v; want ErrNotFound", got, err)
		}
		return ""
	},
}
