package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/livehub"
	"github.com/MerhiOPS/sentrix/internal/infra/memstore"
	"github.com/MerhiOPS/sentrix/internal/infra/password"
)

const testOrigin = "http://sentrix.test"

// tenantRig is one tenant with an admin, a viewer and an enrolled host.
type tenantRig struct {
	scope  domain.Scope
	tenant domain.TenantID
	host   domain.HostID
	admin  string // email
	viewer string
}

type rig struct {
	store  *memstore.Store
	hub    *livehub.Hub
	ingest *ingest.Service
	srv    *httptest.Server
	router routers.Router
	a, b   tenantRig
}

const testPassword = "correct horse battery staple"

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	st := memstore.New()
	hasher := password.New(4)
	sessions, err := identity.NewSessions(ctx, st, st, hasher, identity.NewLoginLimiter(5, time.Minute, 1000, time.Now), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{store: st, hub: livehub.New()}
	r.ingest = ingest.NewService(st, ingest.DefaultWindow, time.Now)
	r.ingest.SetPublisher(r.hub)
	r.a = seedTenant(t, st, hasher, "a")
	r.b = seedTenant(t, st, hasher, "b")
	api := New(Config{
		Sessions: sessions, Users: st, Tokens: identity.NewEnrollment(st, time.Now),
		Hosts: st, Series: query.NewMetrics(st, st), Live: r.hub,
		FormatToken: func(b []byte) string { return "stx1.test" }, Now: time.Now,
	})
	r.srv = httptest.NewServer(api)
	t.Cleanup(r.srv.Close)
	r.router = specRouter(t)
	return r
}

func seedTenant(t *testing.T, st *memstore.Store, h identity.PasswordHasher, name string) tenantRig {
	t.Helper()
	ctx := context.Background()
	tn, err := identity.NewTenants(st, time.Now).Create(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := domain.NewScope(tn.ID)
	users := identity.NewUsers(st, h, time.Now)
	tr := tenantRig{scope: sc, tenant: tn.ID, admin: name + "-admin@example.com", viewer: name + "-viewer@example.com"}
	if _, err := users.Create(ctx, sc, tr.admin, testPassword, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Create(ctx, sc, tr.viewer, testPassword, domain.RoleViewer); err != nil {
		t.Fatal(err)
	}
	enr := identity.NewEnrollment(st, time.Now)
	secret, _, _ := enr.IssueToken(ctx, sc)
	host, _, err := enr.Enroll(ctx, secret, identity.HostInfo{Hostname: name + "-web", OS: "linux", Interval: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	tr.host = host.ID
	return tr
}

// specRouter loads docs/openapi.yaml with an absolute server URL so routes
// can be matched against test requests.
func specRouter(t *testing.T) routers.Router {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml is invalid: %v", err)
	}
	doc.Servers = openapi3.Servers{{URL: "http://sentrix.test/api/v1"}}
	r, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type resp struct {
	status int
	body   []byte
	header http.Header
}

// call sends a request with the session cookie and Origin, and validates
// the response against the OpenAPI spec when the route is in it.
func (r *rig) call(t *testing.T, method, path, cookie, body string) resp {
	t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Origin", testOrigin)
	req.Host = "sentrix.test"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(res.Body)
	r.validate(t, method, path, body, res, data)
	return resp{status: res.StatusCode, body: data, header: res.Header}
}

func (r *rig) validate(t *testing.T, method, path, body string, res *http.Response, data []byte) {
	t.Helper()
	specReq, _ := http.NewRequest(method, "http://sentrix.test"+path, strings.NewReader(body))
	route, params, err := r.router.FindRoute(specReq)
	if err != nil {
		return // not a documented route (e.g. a 404 probe)
	}
	// kin-openapi accepts a status the spec does not list; the contract does
	// not: every status a handler returns must be documented.
	if route.Operation.Responses.Status(res.StatusCode) == nil && route.Operation.Responses.Default() == nil {
		t.Errorf("%s %s → %d does not match docs/openapi.yaml: status not documented", method, path, res.StatusCode)
		return
	}
	in := &openapi3filter.RequestValidationInput{Request: specReq, PathParams: params, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
	out := &openapi3filter.ResponseValidationInput{RequestValidationInput: in, Status: res.StatusCode,
		Header: res.Header, Body: io.NopCloser(bytes.NewReader(data))}
	if err := openapi3filter.ValidateResponse(context.Background(), out); err != nil {
		t.Errorf("%s %s → %d does not match docs/openapi.yaml: %v\nbody: %s", method, path, res.StatusCode, err, data)
	}
}

// login returns the session cookie value for email.
func (r *rig) login(t *testing.T, email string) string {
	t.Helper()
	res := r.call(t, "POST", "/api/v1/auth/login", "", `{"email":"`+email+`","password":"`+testPassword+`"}`)
	if res.status != http.StatusOK {
		t.Fatalf("login %s: %d %s", email, res.status, res.body)
	}
	for _, c := range (&http.Response{Header: res.header}).Cookies() {
		if c.Name == CookieName {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("session cookie flags: %+v", c)
			}
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

// ingestSamples stores samples for a tenant's host through the real use case
// (so the hub is fed after commit, as in production).
func (r *rig) ingestSamples(t *testing.T, tr tenantRig, seq uint64, at ...time.Time) {
	t.Helper()
	b := ingest.Batch{Seq: seq}
	for _, ts := range at {
		b.Samples = append(b.Samples, domain.CoreSample{TS: ts, CPUPercent: 42, MemoryPercent: 10})
	}
	ah := ingest.AuthHost{Tenant: tr.tenant, Host: tr.host, Interval: 5 * time.Second, Enabled: true}
	if _, err := r.ingest.Ingest(context.Background(), ah, b); err != nil {
		t.Fatal(err)
	}
}
