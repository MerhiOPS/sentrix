package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
	"github.com/MerhiOPS/sentrix/internal/infra/tlscert"
)

// dbFile is the database's name inside the data directory.
const dbFile = "sentrix.db"

// openStore opens and migrates the data directory's database.
func openStore(ctx context.Context, dataDir string) (*sqlite.Store, func() error, error) {
	if dataDir == "" {
		return nil, nil, errors.New("--data is required")
	}
	db, err := sqlite.OpenMigrated(ctx, filepath.Join(dataDir, dbFile))
	if err != nil {
		return nil, nil, err
	}
	return sqlite.NewStore(db), db.Close, nil
}

// tlsFlags are the certificate options shared by serve and token create.
type tlsFlags struct {
	certFile, keyFile string
}

func (t *tlsFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&t.certFile, "tls-cert", "", "CA-issued certificate (PEM); default: self-signed key in --data")
	fs.StringVar(&t.keyFile, "tls-key", "", "private key for --tls-cert (PEM)")
}

// caMode reports whether an operator-supplied certificate is in use. In CA
// mode tokens carry no pin: pinning a CA leaf would brick agents at renewal.
func (t tlsFlags) caMode() bool { return t.certFile != "" }

// load returns the serving certificate and, in self-signed mode, its pin.
func (t tlsFlags) load(dataDir string) (tls.Certificate, []byte, error) {
	if t.caMode() {
		if t.keyFile == "" {
			return tls.Certificate{}, nil, errors.New("--tls-cert needs --tls-key")
		}
		c, err := tls.LoadX509KeyPair(t.certFile, t.keyFile)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load tls certificate: %w", err)
		}
		return c, nil, nil
	}
	c, err := tlscert.LoadOrCreate(dataDir)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("self-signed certificate: %w", err)
	}
	pin, err := tlscert.LeafPin(c)
	return c, pin, err
}
