package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type issued struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// issue creates a certificate valid [from, until), signed by parent (self
// when nil), for 127.0.0.1 unless names says otherwise.
func issue(t *testing.T, parent *issued, ca bool, from, until time.Time, ips ...net.IP) issued {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "sentrix test"},
		NotBefore: from, NotAfter: until, IsCA: ca, BasicConstraintsValid: true, IPAddresses: ips,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return issued{c, key}
}

// tlsServer serves https with leaf (chained through intermediate when set).
func tlsServer(t *testing.T, leaf issued, intermediate *issued) *httptest.Server {
	t.Helper()
	chain := [][]byte{leaf.cert.Raw}
	if intermediate != nil {
		chain = append(chain, intermediate.cert.Raw)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: chain, PrivateKey: leaf.key}}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestTLSCertificates(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	localhost := net.IPv4(127, 0, 0, 1)
	root := issue(t, nil, true, now.Add(-day), now.Add(3650*day))
	inter := issue(t, &root, true, now.Add(-day), now.Add(20*day)) // expires before the leaf
	pool := x509.NewCertPool()
	pool.AddCert(root.cert)
	p := loopback()
	p.roots = pool

	good := tlsServer(t, issue(t, &inter, false, now.Add(-day), now.Add(90*day), localhost), &inter)
	r := p.Probe(context.Background(), httpMon(good.URL))
	expect(t, r, domain.CheckUp, domain.ErrNone)
	if r.CertExpiry == nil || !r.CertExpiry.Equal(inter.cert.NotAfter) {
		t.Fatalf("cert expiry %v, want the intermediate's %v: the earliest in the chain", r.CertExpiry, inter.cert.NotAfter)
	}
	bad := map[string]*httptest.Server{
		"expired":    tlsServer(t, issue(t, &inter, false, now.Add(-10*day), now.Add(-day), localhost), &inter),
		"wrong name": tlsServer(t, issue(t, &inter, false, now.Add(-day), now.Add(90*day), net.IPv4(10, 9, 8, 7)), &inter),
		"untrusted":  tlsServer(t, issue(t, nil, false, now.Add(-day), now.Add(90*day), localhost), nil),
	}
	for name, srv := range bad {
		r := p.Probe(context.Background(), httpMon(srv.URL))
		if r.Outcome != domain.CheckDown || r.Class != domain.ErrTLS {
			t.Errorf("%s certificate: %+v, want down/tls", name, r)
		}
	}
	plain := httptest.NewServer(http.NotFoundHandler())
	defer plain.Close()
	expect(t, p.Probe(context.Background(), httpMon("https://"+plain.Listener.Addr().String()+"/")), domain.CheckDown, domain.ErrTLS)
}
