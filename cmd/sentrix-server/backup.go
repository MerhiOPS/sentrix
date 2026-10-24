package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/compliance"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

// backupCmd: sentrix-server backup --data DIR --out FILE
// Safe while the server runs: VACUUM INTO reads one consistent snapshot.
func backupCmd(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	out := fs.String("out", "", "backup file to create (must not exist)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is required")
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	ops, err := domain.NewOpsScope("backup")
	if err != nil {
		return errors.Join(err, closeDB())
	}
	start := time.Now()
	err = st.Backup(ctx, ops, *out)
	audit(ctx, st, ops, "backup", *out, err)
	if err != nil {
		return errors.Join(err, closeDB())
	}
	fmt.Printf("backup written to %s in %v\n", *out, time.Since(start).Round(time.Millisecond))
	fmt.Println("keep server-key.pem with it: agents are pinned to the server's key")
	return closeDB()
}

// restoreCmd: sentrix-server restore --from FILE --data NEW-DIR
func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	from := fs.String("from", "", "backup file")
	data := fs.String("data", "", "data directory to restore into (must hold no database)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *data == "" {
		return errors.New("--from and --data are required")
	}
	ctx := context.Background()
	start := time.Now()
	if err := sqlite.Restore(ctx, *from, *data, dbFile); err != nil {
		return err
	}
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	if ops, err := domain.NewOpsScope("restore"); err == nil {
		audit(ctx, st, ops, "restore", *from, nil)
	}
	fmt.Printf("restored into %s in %v (integrity check ok); copy server-key.pem and server-cert.pem there too\n",
		*data, time.Since(start).Round(time.Millisecond))
	return closeDB()
}

// audit records an operator action; a failure to audit is reported, not fatal.
func audit(ctx context.Context, st *sqlite.Store, ops domain.OpsScope, action, subject string, err error) {
	sum := sha256.Sum256([]byte(subject))
	e := compliance.AuditEntry{At: time.Now().UTC(), Action: action, SubjectHash: hex.EncodeToString(sum[:]), Outcome: "ok"}
	if err != nil {
		e.Outcome, e.Detail = "failed", err.Error()
	}
	if aerr := st.AppendAudit(ctx, ops, e); aerr != nil {
		fmt.Println("warning: audit entry not written:", aerr)
	}
}
