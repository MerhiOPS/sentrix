// Command sentrix-server runs the Sentrix server and its admin commands.
//
//	sentrix-server serve        --data DIR [--listen :8443] [--insecure] [--tls-cert F --tls-key F]
//	sentrix-server tenant create --data DIR --name NAME
//	sentrix-server token create  --data DIR --tenant ID [--tls-cert F]
//	sentrix-server user create   --data DIR --tenant ID --email E [--role admin|viewer]
package main

import (
	"fmt"
	"os"
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
		return subcommand(rest, "create", tenantCreate)
	case "token":
		return subcommand(rest, "create", tokenCreate)
	case "user":
		return subcommand(rest, "create", userCreate)
	case "version":
		fmt.Println(version)
		return nil
	default:
		return usage()
	}
}

func subcommand(args []string, name string, fn func([]string) error) error {
	if len(args) == 0 || args[0] != name {
		return usage()
	}
	return fn(args[1:])
}

func usage() error {
	return fmt.Errorf("usage: sentrix-server serve|tenant create|token create|user create|version (see --help on each)")
}

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"
