package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// tenantCreate: sentrix-server tenant create --data DIR --name NAME
func tenantCreate(args []string) error {
	fs := flag.NewFlagSet("tenant create", flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	name := fs.String("name", "", "tenant name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	tn, err := identity.NewTenants(st, time.Now).Create(ctx, *name)
	return errors.Join(printTenant(tn, err), closeDB())
}

func printTenant(tn domain.Tenant, err error) error {
	if err != nil {
		return err
	}
	fmt.Printf("tenant %q created: %s\n", tn.Name, tn.ID)
	return nil
}

// tokenCreate: sentrix-server token create --data DIR --tenant ID
func tokenCreate(args []string) error {
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	tenant := fs.String("tenant", "", "tenant id")
	var tf tlsFlags
	tf.register(fs)
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
	var pin []byte
	if !tf.caMode() {
		if _, pin, err = tf.load(*data); err != nil {
			return err
		}
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	secret, expires, err := identity.NewEnrollment(st, time.Now).IssueToken(ctx, scope)
	if err != nil {
		return errors.Join(err, closeDB())
	}
	fmt.Println(wire.Token{Secret: secret, Pin: pin})
	fmt.Printf("single use, expires %s\n", expires.Local().Format(time.RFC1123))
	return closeDB()
}
