package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/compliance"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// eraseCmd: sentrix-server erase host|user|tenant --data DIR --tenant ID
// [--host ID | --user ID] --yes
//
// Deletes the subject with everything that belongs to it, then proves it
// with a canary byte-scan of the database and WAL. Run it with the server
// stopped: the WAL can only be truncated when no reader holds it.
func eraseCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sentrix-server erase host|user|tenant --data DIR --tenant ID [--host ID|--user ID] --yes")
	}
	kind := compliance.SubjectKind(args[0])
	fs := flag.NewFlagSet("erase "+args[0], flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	tenant := fs.String("tenant", "", "tenant id")
	host := fs.String("host", "", "host id (erase host)")
	user := fs.String("user", "", "user id (erase user)")
	yes := fs.Bool("yes", false, "confirm: this cannot be undone")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	sub, err := subjectFrom(kind, *tenant, *host, *user)
	if err != nil {
		return err
	}
	if !*yes {
		return errors.New("erasure cannot be undone: pass --yes")
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	rep, err := compliance.NewEraser(st, st, time.Now).Erase(ctx, sub)
	if err != nil {
		return errors.Join(err, closeDB())
	}
	fmt.Printf("erased %s; post-check clean (%d identifiers scanned for in the database and WAL); audit subject %s\n",
		kind, rep.Needles, rep.SubjectHash)
	fmt.Println("note: backups taken before now still hold this data until they expire")
	return closeDB()
}

func subjectFrom(kind compliance.SubjectKind, tenant, host, user string) (compliance.Subject, error) {
	tid, err := domain.ParseTenantID(tenant)
	if err != nil {
		return compliance.Subject{}, fmt.Errorf("--tenant: %w", err)
	}
	s := compliance.Subject{Kind: kind, Tenant: tid}
	switch kind {
	case compliance.SubjectHost:
		if s.Host, err = domain.ParseHostID(host); err != nil {
			return s, fmt.Errorf("--host: %w", err)
		}
	case compliance.SubjectUser:
		if s.User, err = domain.ParseUserID(user); err != nil {
			return s, fmt.Errorf("--user: %w", err)
		}
	case compliance.SubjectTenant:
	default:
		return s, fmt.Errorf("erase what? host, user or tenant, not %q", kind)
	}
	return s, nil
}
