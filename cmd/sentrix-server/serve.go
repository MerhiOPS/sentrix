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
	"strings"
	"syscall"

	"github.com/MerhiOPS/sentrix/internal/infra/ingesthttp"
	"github.com/MerhiOPS/sentrix/internal/infra/tlscert"
)

type serveFlags struct {
	data, listen, apiListen, origins string
	insecure, apiTLS                 bool
	tls                              tlsFlags
}

func parseServe(args []string) (serveFlags, error) {
	var f serveFlags
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&f.data, "data", "", "data directory")
	fs.StringVar(&f.listen, "listen", ":8443", "agent ingest address (TLS)")
	fs.BoolVar(&f.insecure, "insecure", false, "plain HTTP ingest (loopback addresses only)")
	fs.StringVar(&f.apiListen, "api-listen", "127.0.0.1:8080", "dashboard address")
	fs.BoolVar(&f.apiTLS, "api-tls", false, "serve the dashboard over TLS (required off loopback)")
	fs.StringVar(&f.origins, "api-origins", "", "extra allowed origins, comma-separated (dev server)")
	f.tls.register(fs)
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if err := ingesthttp.CheckListen(f.listen, f.insecure); err != nil {
		return f, err
	}
	if err := ingesthttp.CheckListen(f.apiListen, !f.apiTLS); err != nil {
		return f, errors.New("dashboard: " + err.Error() + " — pass --api-tls or keep it on loopback behind a TLS proxy")
	}
	return f, nil
}

// serve runs the ingest and dashboard listeners until SIGINT/SIGTERM.
func serve(args []string) error {
	f, err := parseServe(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, closeDB, err := openStore(ctx, f.data)
	if err != nil {
		return err
	}
	var tlsCfg *tls.Config
	var pin []byte
	if !f.insecure || f.apiTLS {
		var cert tls.Certificate
		if cert, pin, err = f.tls.load(f.data); err != nil {
			return errors.Join(err, closeDB())
		}
		tlsCfg = tlscert.ServerConfig(cert)
		if pin != nil {
			slog.Info("self-signed TLS", "spki_pin", hex.EncodeToString(pin))
		}
	}
	h, err := build(ctx, st, pin, splitList(f.origins))
	if err != nil {
		return errors.Join(err, closeDB())
	}
	ingestSrv := ingesthttp.NewServer(f.listen, h.ingest, pick(!f.insecure, tlsCfg))
	apiSrv := ingesthttp.NewServer(f.apiListen, h.dashboard, pick(f.apiTLS, tlsCfg))
	return errors.Join(runServers(ctx, ingestSrv, apiSrv), closeDB())
}

func pick(on bool, cfg *tls.Config) *tls.Config {
	if on {
		return cfg
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runServers serves until ctx ends or one listener fails, then shuts both
// down gracefully: in-flight batches commit before the database closes.
func runServers(ctx context.Context, srvs ...*http.Server) error {
	errc := make(chan error, len(srvs))
	for _, s := range srvs {
		go func(s *http.Server) {
			slog.Info("listening", "addr", s.Addr, "tls", s.TLSConfig != nil)
			var err error
			if s.TLSConfig != nil {
				err = s.ListenAndServeTLS("", "")
			} else {
				err = s.ListenAndServe()
			}
			if !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}(s)
	}
	var first error
	select {
	case first = <-errc:
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, s := range srvs {
		first = errors.Join(first, s.Shutdown(sctx))
	}
	return first
}
