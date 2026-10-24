// Command sentrix-agent collects host metrics and ships them to a Sentrix
// server.
//
//	sentrix-agent enroll --server https://HOST:8443 --token TOKEN [--state-dir DIR]
//	sentrix-agent run    [--state-dir DIR] [--interval 5s] [--log-file GLOB]... [--journald] [--eventlog CHANNELS]
//	sentrix-agent version
//	sentrix-agent install   [run flags]   (Windows: register and start the service)
//	sentrix-agent uninstall               (Windows)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/buildinfo"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func main() {
	if inService() { // started by the Windows Service Control Manager
		if err := runService(os.Args[1:]); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sentrix-agent:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sentrix-agent enroll|run|install|uninstall|version")
	}
	switch args[0] {
	case "enroll":
		return enroll(args[1:])
	case "run":
		return runAgent(args[1:])
	case "install":
		return install(args[1:])
	case "uninstall":
		return uninstall()
	case "version":
		fmt.Println(buildinfo.Describe(agent.Version))
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
