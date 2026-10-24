package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// maskSecrets is a fake redactor: replaces "secret" and drops "DROPME".
type maskSecrets struct{}

func (maskSecrets) Redact(_ context.Context, _ domain.Scope, in []domain.LogLine) ([]domain.LogLine, int, int, error) {
	var out []domain.LogLine
	red, drop := 0, 0
	for _, l := range in {
		if strings.Contains(l.Message, "DROPME") {
			drop++
			continue
		}
		if strings.Contains(l.Message, "secret") {
			l.Message = strings.ReplaceAll(l.Message, "secret", "[REDACTED]")
			red++
		}
		out = append(out, l)
	}
	return out, red, drop, nil
}

type disk bool

func (d disk) LogsAllowed() bool { return bool(d) }

func logLine(sec int, msg string) domain.LogLine {
	return domain.LogLine{TS: t0.Add(time.Duration(sec) * time.Second), Source: "app", Level: domain.LevelInfo, Message: msg}
}

func TestLogsRedactedBeforeStorage(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	svc.SetLogPipeline(maskSecrets{}, nil)
	_, err := svc.Ingest(context.Background(), authHost(), Batch{Seq: 1, Logs: []domain.LogLine{
		logLine(0, "the secret is x"), logLine(1, "DROPME please"), logLine(2, "fine"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	p := st.prepared[0]
	if len(p.Logs) != 2 || strings.Contains(p.Logs[0].Message, "secret") {
		t.Fatalf("stored logs: %+v", p.Logs)
	}
	if p.LogStats.Redacted != 1 || p.LogStats.Dropped != 1 || p.LogStats.Received != 3 {
		t.Fatalf("stats: %+v", p.LogStats)
	}
}

func TestOverQuotaShedsLogsKeepsMetrics(t *testing.T) {
	st := newFake()
	st.budget = int64(logLine(0, "0123456789").Size()) // room for exactly one line
	svc := newSvc(st)
	svc.SetLogPipeline(maskSecrets{}, nil)
	res, err := svc.Ingest(context.Background(), authHost(), Batch{Seq: 1,
		Samples: []domain.CoreSample{sample(0, 1)},
		Logs:    []domain.LogLine{logLine(0, "0123456789"), logLine(1, "0123456789"), logLine(2, "0123456789")}})
	if err != nil {
		t.Fatal(err)
	}
	p := st.prepared[0]
	if len(p.Samples) != 1 || len(p.Logs) != 1 || res.LogsShed != 2 || res.LogsShedReason != ShedQuota {
		t.Fatalf("samples %d logs %d shed %d (%s); want metrics kept, 2 lines shed for quota",
			len(p.Samples), len(p.Logs), res.LogsShed, res.LogsShedReason)
	}
	if p.LogStats.Bytes != st.budget {
		t.Fatalf("metered %d bytes, want %d (only the stored line)", p.LogStats.Bytes, st.budget)
	}
}

func TestBudgetAlreadyNegativeShedsEverything(t *testing.T) {
	st := newFake()
	st.budget = -500
	svc := newSvc(st)
	svc.SetLogPipeline(maskSecrets{}, nil)
	res, _ := svc.Ingest(context.Background(), authHost(), Batch{Seq: 1, Logs: []domain.LogLine{logLine(0, "x")}})
	if res.LogsShed != 1 || len(st.prepared[0].Logs) != 0 {
		t.Fatalf("over-budget tenant stored logs: %+v", res)
	}
}

func TestLowDiskShedsLogsKeepsMetrics(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	svc.SetLogPipeline(maskSecrets{}, disk(false))
	res, err := svc.Ingest(context.Background(), authHost(), Batch{Seq: 1,
		Samples: []domain.CoreSample{sample(0, 1)}, Logs: []domain.LogLine{logLine(0, "x")}})
	if err != nil || res.LogsShedReason != ShedDisk || res.LogsShed != 1 || res.Stored != 1 {
		t.Fatalf("low disk: %+v %v", res, err)
	}
}

// TestNoRedactorShedsNeverFails: unredacted logs are never stored, and the
// batch must not fail — a failed batch would cost the host its metrics.
func TestNoRedactorShedsNeverFails(t *testing.T) {
	st := newFake()
	res, err := newSvc(st).Ingest(context.Background(), authHost(), Batch{Seq: 1,
		Samples: []domain.CoreSample{sample(0, 1)}, Logs: []domain.LogLine{logLine(0, "secret")}})
	if err != nil || res.LogsShedReason != ShedDisabled || len(st.prepared[0].Logs) != 0 || res.Stored != 1 {
		t.Fatalf("no redactor: %+v %v", res, err)
	}
}

func TestAllDroppedBatchIsStillMetered(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	svc.SetLogPipeline(maskSecrets{}, nil)
	if _, err := svc.Ingest(context.Background(), authHost(), Batch{Seq: 1, Logs: []domain.LogLine{logLine(0, "DROPME")}}); err != nil {
		t.Fatal(err)
	}
	if len(st.prepared) != 1 || st.prepared[0].LogStats.Dropped != 1 {
		t.Fatal("a batch whose every line was dropped was not committed with its counts (OLD C-M11)")
	}
}

func TestLogsPublishedAfterCommit(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	svc.SetLogPipeline(maskSecrets{}, nil)
	pub := &recPub{}
	svc.SetPublisher(pub)
	if _, err := svc.Ingest(context.Background(), authHost(), Batch{Seq: 1, Logs: []domain.LogLine{logLine(0, "a secret")}}); err != nil {
		t.Fatal(err)
	}
	if len(pub.logs) != 1 || strings.Contains(pub.logs[0][0].Message, "secret") {
		t.Fatalf("published logs: %+v (must be the redacted, stored lines)", pub.logs)
	}
}
