package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/collector"
)

// runOptions are the parsed flags of `run`.
type runOptions struct {
	dir      string
	interval time.Duration
	debug    bool
	logs     logFlags
}

func parseRun(args []string) (runOptions, error) {
	var o runOptions
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.StringVar(&o.dir, "state-dir", defaultStateDir, "state directory")
	fs.DurationVar(&o.interval, "interval", 5*time.Second, "sampling interval")
	fs.BoolVar(&o.debug, "debug", false, "log collector errors")
	o.logs.register(fs)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	return o, domain.ValidateInterval(o.interval)
}

// runAgent is `sentrix-agent run` from a terminal or a systemd unit: it
// stops on SIGINT/SIGTERM.
func runAgent(args []string) error {
	o, err := parseRun(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWith(ctx, o, os.Stderr, false)
}

// runWith runs the agent until ctx ends. asService protects the credential
// under the running account first (Windows DPAPI; a no-op elsewhere).
func runWith(ctx context.Context, o runOptions, logOut io.Writer, asService bool) error {
	level := slog.LevelInfo
	if o.debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: level}))
	if asService {
		if changed, err := agent.ProtectIdentity(o.dir); err != nil {
			return err
		} else if changed {
			log.Info("credential encrypted under the service account")
		}
	}
	logSources, err := o.logs.sources()
	if err != nil {
		return err
	}
	id, err := agent.LoadIdentity(o.dir)
	if err != nil {
		return err
	}
	cfg := agent.DefaultConfig(o.dir)
	cfg.Interval = o.interval
	cfg.BatchEvery = max(2*o.interval, 10*time.Second)
	a, err := agent.New(cfg, id, agent.Sources{
		Core: collector.NewCore(), Detail: collector.NewDetail(), Procs: collector.NewProcesses(), Logs: logSources,
	}, log)
	if err != nil {
		return err
	}
	log.Info("agent running", "version", agent.Version, "server", id.ServerURL, "host_id", id.HostID, "interval", o.interval)
	a.Run(ctx)
	return nil
}
