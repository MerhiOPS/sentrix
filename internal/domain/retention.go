package domain

import "time"

// DataKind is a class of stored data with its own retention.
type DataKind string

const (
	DataRawCore   DataKind = "metrics_raw"
	DataDetail    DataKind = "detail"
	DataProcesses DataKind = "processes"
	DataRollup1m  DataKind = "rollup_1m"
	DataRollup1h  DataKind = "rollup_1h"
	DataLogs      DataKind = "logs"
)

// DataKinds lists every kind the janitor prunes.
var DataKinds = []DataKind{DataRawCore, DataDetail, DataProcesses, DataRollup1m, DataRollup1h, DataLogs}

// Retention is how long each kind is kept. Charts keep working past the raw
// horizon because they switch to rollups (query.plan), so raw data can be
// short-lived while hourly history lasts a year.
type Retention map[DataKind]time.Duration

const day = 24 * time.Hour

// DefaultRetention applies to every tenant until plans (Phase 7) set their
// own. Raw core outlives the agent's 72 h replay window, so a replayed
// batch never lands in already-pruned time.
var DefaultRetention = Retention{
	DataRawCore:   7 * day,
	DataDetail:    7 * day,
	DataProcesses: 3 * day,
	DataRollup1m:  30 * day,
	DataRollup1h:  400 * day,
	DataLogs:      14 * day,
}
