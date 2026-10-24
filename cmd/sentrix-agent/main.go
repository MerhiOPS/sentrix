// Command sentrix-agent collects host metrics and ships them to a Sentrix
// server.
//
//	sentrix-agent enroll --server https://HOST:8443 --token TOKEN [--state-dir DIR]
//	sentrix-agent run    [--state-dir DIR] [--interval 5s] [--log-file GLOB]... [--journald] [--eventlog CHANNELS]
//	sentrix-agent version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/collector"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sentrix-agent:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sentrix-agent enroll|run|version")
	}
	switch args[0] {
	case "enroll":
		return enroll(args[1:])
	case "run":
		return runAgent(args[1:])
	case "version":
		fmt.Println(agent.Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func enroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	server := fs.String("server", "", "server URL, e.g. https://monitor.example.com:8443")
	token := fs.String("token", "", "enrollment token from `sentrix-server token create`")
	dir := fs.String("state-dir", defaultStateDir, "state directory")
	interval := fs.Duration("interval", 5*time.Second, "sampling interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" || *token == "" {
		return errors.New("--server and --token are required")
	}
	if err := domain.ValidateInterval(*interval); err != nil {
		return err
	}
	id, err := agent.Enroll(context.Background(), *dir, *server, *token, *interval)
	if err != nil {
		return err
	}
	fmt.Printf("enrolled as host %s; start the agent with `sentrix-agent run`\n", id.HostID)
	return nil
}

func runAgent(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dir := fs.String("state-dir", defaultStateDir, "state directory")
	interval := fs.Duration("interval", 5*time.Second, "sampling interval")
	debug := fs.Bool("debug", false, "log collector errors")
	var lf logFlags
	lf.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	logSources, err := lf.sources()
	if err != nil {
		return err
	}
	if err := domain.ValidateInterval(*interval); err != nil {
		return err
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	id, err := agent.LoadIdentity(*dir)
	if err != nil {
		return err
	}
	cfg := agent.DefaultConfig(*dir)
	cfg.Interval = *interval
	cfg.BatchEvery = max(2**interval, 10*time.Second)
	a, err := agent.New(cfg, id, agent.Sources{
		Core: collector.NewCore(), Detail: collector.NewDetail(), Procs: collector.NewProcesses(), Logs: logSources,
	}, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("agent running", "version", agent.Version, "server", id.ServerURL, "host_id", id.HostID, "interval", *interval)
	a.Run(ctx)
	return nil
}
