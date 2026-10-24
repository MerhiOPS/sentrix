package storetest

import (
	"context"
	"testing"
)

// Run executes the whole contract against fresh adapters from newStores.
func Run(t *testing.T, newStores Factory) {
	cases := map[string]func(*testing.T, Stores){
		"tenant/create-and-get":             testTenantCreateGet,
		"tenant/duplicate-conflicts":        testTenantDuplicate,
		"tenant/unknown-scope":              testUnknownScope,
		"enroll/single-use":                 testTokenSingleUse,
		"enroll/expired":                    testTokenExpired,
		"enroll/credential-resolves":        testCredentialResolves,
		"ingest/stores-and-reads":           testIngestStoresAndReads,
		"ingest/replay-applies-nothing":     testReplayAppliesNothing,
		"ingest/conflicts-counted":          testConflictsCounted,
		"ingest/build-error-rolls-back":     testBuildErrorRollsBack,
		"ingest/counter-state-persists":     testCounterStatePersists,
		"ingest/seq-pruned-after-retention": testSeqPruned,
		"ingest/last-seen-updated":          testLastSeenUpdated,
		"ingest/gauge-precision":            testGaugePrecision,
		"ingest/health-persisted":           testHealthPersisted,
		"ingest/latest-never-backwards":     testLatestNeverGoesBackwards,
		"users/email-unique-across-tenants": testUserEmailUniqueAcrossTenants,
		"users/find-login":                  testFindLoginReturnsTenantAndHash,
		"sessions/lifecycle":                testSessionLifecycle,
		"depth/rollups-equal-raw":           testRollupsEqualAggregationOfRaw,
		"depth/detail-series-precision":     testDetailSeriesAndPrecision,
		"depth/series-ceiling":              testSeriesCeilingRefusesOnlyNewSeries,
		"depth/series-cache-rollback":       testSeriesCreatedInRolledBackBatchIsNotCached,
		"depth/processes-at":                testProcessesAtReturnsNewestNotAfter,
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { c(t, newStores(t)) })
	}
	t.Run("isolation/cross-tenant", func(t *testing.T) {
		for _, p := range CrossTenantProblems(context.Background(), t, newStores(t)) {
			t.Error(p)
		}
	})
}
