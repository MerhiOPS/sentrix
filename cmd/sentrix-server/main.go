// Command sentrix-server runs the Sentrix server and its admin commands.
//
//	sentrix-server serve        --data DIR [--listen :8443] [--insecure] [--tls-cert F --tls-key F]
//	sentrix-server tenant create --data DIR --name NAME
//	sentrix-server tenant plan   --data DIR --tenant ID [--max-hosts N] [--log-mb-per-day N] [--metrics-days N] [--logs-days N]
//	sentrix-server token create  --data DIR --tenant ID [--tls-cert F]
//	sentrix-server user create   --data DIR --tenant ID --email E [--role admin|viewer]
package main

import (
	"fmt"
	"os"

	"github.com/MerhiOPS/sentrix/internal/buildinfo"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sentrix-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "serve":
		return serve(rest)
	case "tenant":
		return subcommand(rest, map[string]func([]string) error{"create": tenantCreate, "plan": tenantPlan})
	case "token":
		return subcommand(rest, map[string]func([]string) error{"create": tokenCreate})
	case "user":
		return subcommand(rest, map[string]func([]string) error{"create": userCreate})
	case "version":
		fmt.Println(buildinfo.Describe(version))
		return nil
	default:
		return usage()
	}
}

func subcommand(args []string, cmds map[string]func([]string) error) error {
	if len(args) == 0 || cmds[args[0]] == nil {
		return usage()
	}
	return cmds[args[0]](args[1:])
}

func usage() error {
	return fmt.Errorf("usage: sentrix-server serve|tenant create|tenant plan|token create|user create|version (see --help on each)")
}

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"
