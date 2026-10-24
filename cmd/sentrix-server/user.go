package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/password"
)

// userCreate: sentrix-server user create --data DIR --tenant ID --email E [--role admin|viewer]
// The password is read from the terminal without echo, or from stdin when
// piped — never from a flag, where it would land in shell history.
func userCreate(args []string) error {
	fs := flag.NewFlagSet("user create", flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	tenant := fs.String("tenant", "", "tenant id")
	email := fs.String("email", "", "login email")
	roleName := fs.String("role", "admin", "admin or viewer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tid, err := domain.ParseTenantID(*tenant)
	if err != nil {
		return fmt.Errorf("--tenant: %w", err)
	}
	scope, err := domain.NewScope(tid)
	if err != nil {
		return err
	}
	role, err := domain.ParseRole(*roleName)
	if err != nil {
		return err
	}
	pw, err := readPassword()
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	u, err := identity.NewUsers(st, password.New(1), time.Now).Create(ctx, scope, *email, pw, role)
	if err != nil {
		return errors.Join(err, closeDB())
	}
	fmt.Printf("user %s (%s) created: %s\n", u.Email, u.Role, u.ID)
	return closeDB()
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // stdin's descriptor fits an int
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "again: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("passwords do not match")
	}
	return string(a), nil
}
