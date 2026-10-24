package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/infra/ingesthttp"
	"github.com/MerhiOPS/sentrix/internal/infra/tlscert"
)

// Credential cache: a disabled host can keep sending for at most authTTL.
const (
	authTTL   = 30 * time.Second
	authCache = 10_000
)

// serve: sentrix-server serve --data DIR [--listen ADDR] [--insecure]
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	listen := fs.String("listen", ":8443", "ingest listen address")
	insecure := fs.Bool("insecure", false, "plain HTTP (loopback addresses only)")
	var tf tlsFlags
	tf.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := ingesthttp.CheckListen(*listen, *insecure); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	auth := ingest.NewAuthenticator(st, authTTL, authCache, time.Now)
	svc := ingest.NewService(st, ingest.DefaultWindow, time.Now)
	h := ingesthttp.NewHandler(auth, svc, identity.NewEnrollment(st, time.Now))

	var tlsCfg *tls.Config
	if !*insecure {
		cert, pin, err := tf.load(*data)
		if err != nil {
			return errors.Join(err, closeDB())
		}
		tlsCfg = tlscert.ServerConfig(cert)
		if pin != nil {
			slog.Info("self-signed TLS", "spki_pin", hex.EncodeToString(pin))
		}
	}
	srv := ingesthttp.NewServer(*listen, h, tlsCfg)
	return errors.Join(runServer(ctx, srv), closeDB())
}

// runServer serves until ctx ends, then shuts down gracefully: in-flight
// batches finish (and commit) before the database closes.
func runServer(ctx context.Context, srv *http.Server) error {
	errc := make(chan error, 1)
	go func() {
		slog.Info("ingest listening", "addr", srv.Addr, "tls", srv.TLSConfig != nil)
		var err error
		if srv.TLSConfig != nil {
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}
