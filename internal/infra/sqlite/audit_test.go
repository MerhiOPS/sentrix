package sqlite

import (
	"regexp"
	"strings"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/infra/storetest"
)

// tenantTables are tables whose rows belong to one tenant.
var tenantTables = regexp.MustCompile(`\b(hosts|enrollment_tokens|ingest_batches|host_counters|metrics_core|latest_core|users|sessions|series|samples_detail|process_snapshots|rollup_core_1m|rollup_core_1h|logs|log_usage|redaction_rules|channels|alert_rules|alert_rule_hosts|alert_rule_channels|alert_states|alert_events|silences)\b`)

// unscopedAllowed are the only statements that may touch tenant tables
// without a tenant predicate, each because it is how the tenant is *found*.
var unscopedAllowed = map[string]string{
	"WHERE h.credential_hash = ?":                                        "agent credential → tenant (AuthenticateCredential)",
	"WHERE secret_hash = ? AND used_at IS NULL":                          "enrollment token → tenant (RedeemToken)",
	"WHERE u.email = ?":                                                  "login email → tenant (FindLogin)",
	"WHERE s.token_hash = ? AND u.disabled_at IS NULL":                   "session cookie → tenant (GetSession)",
	"FROM hosts h JOIN tenants t ON t.key = h.tenant_key ORDER BY h.key": "retention janitor enumerates (tenant, host) under an OpsScope",
	"WHERE notify = 1 AND outcome = ''":                                  "startup sweep of undelivered alerts, every tenant, under an OpsScope",
}

// auditProblems returns every statement that touches a tenant table without
// binding tenant_key (PLAN §4.1 item 5).
func auditProblems(stmts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, q := range stmts {
		norm := strings.Join(strings.Fields(q), " ")
		if seen[norm] || isSchemaStatement(norm) || !tenantTables.MatchString(norm) {
			continue
		}
		seen[norm] = true
		if bindsTenant(norm) || allowedUnscoped(norm) {
			continue
		}
		out = append(out, norm)
	}
	return out
}

// tenantPredicate: tenant_key compared in a WHERE/ON/AND clause.
var tenantPredicate = regexp.MustCompile(`(?i)\b(WHERE|AND|ON)\s+(\w+\.)?tenant_key\s*=`)

// insertWithTenant: an INSERT naming tenant_key among its columns.
var insertWithTenant = regexp.MustCompile(`(?i)^INSERT\b[^(]*\([^)]*\btenant_key\b`)

// bindsTenant reports whether the statement constrains rows by tenant, not
// merely mentions the column (SELECT tenant_key ... WHERE host_key = ? reads
// across tenants and must fail).
func bindsTenant(q string) bool {
	return tenantPredicate.MatchString(q) || insertWithTenant.MatchString(q)
}

func isSchemaStatement(q string) bool {
	u := strings.ToUpper(q)
	for _, p := range []string{"CREATE ", "DROP ", "ALTER ", "PRAGMA ", "-- ", "INSERT INTO SCHEMA_MIGRATIONS", "SELECT NAME, CHECKSUM"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

func allowedUnscoped(q string) bool {
	for frag := range unscopedAllowed {
		if strings.Contains(q, frag) {
			return true
		}
	}
	return false
}

func TestStatementAuditCatchesPlantedQuery(t *testing.T) {
	planted := []string{
		"SELECT ts FROM metrics_core WHERE host_key = ?",         // missing tenant: caught
		"SELECT tenant_key FROM metrics_core WHERE host_key = ?", // mentions, does not bind: caught
		"SELECT key FROM hosts WHERE tenant_key = ? AND id = ?",  // scoped: fine
		"CREATE TABLE metrics_core (x INTEGER)",                  // DDL: ignored
		"SELECT id FROM tenants WHERE key = ?",                   // not a tenant-owned table
	}
	if got := auditProblems(planted); len(got) != 2 {
		t.Fatalf("audit flagged %v, want exactly the two unscoped metrics_core reads", got)
	}
}

// TestStatementAuditOverContract runs the whole contract through the
// recording driver and fails on any unscoped statement the adapter issued.
func TestStatementAuditOverContract(t *testing.T) {
	prev := driverName
	driverName = "sqlite-audit"
	t.Cleanup(func() { driverName = prev })

	before := len(auditRec.all())
	storetest.Run(t, func(t *testing.T) storetest.Stores { return newMigratedStore(t) })
	stmts := auditRec.all()[before:]
	if len(stmts) < 50 {
		t.Fatalf("only %d statements recorded: the audit driver is not in the path", len(stmts))
	}
	for _, p := range auditProblems(stmts) {
		t.Errorf("unscoped statement on a tenant table: %s", p)
	}
}
