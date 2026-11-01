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

	DataMonitorResults   DataKind = "monitor_results"   // one row per check
	DataMonitorHourly    DataKind = "monitor_hourly"    // uptime and latency per hour
	DataMonitorIncidents DataKind = "monitor_incidents" // closed incidents and monitor events
)

// DataKinds lists every per-host kind the janitor prunes.
var DataKinds = []DataKind{DataRawCore, DataDetail, DataProcesses, DataRollup1m, DataRollup1h, DataLogs}

// MonitorDataKinds lists every per-monitor kind the janitor prunes.
var MonitorDataKinds = []DataKind{DataMonitorResults, DataMonitorHourly, DataMonitorIncidents}

// Retention is how long each kind is kept. Charts keep working past the raw
// horizon because they switch to rollups (query.plan), so raw data can be
// short-lived while hourly history lasts a year.
type Retention map[DataKind]time.Duration

const day = 24 * time.Hour

// DefaultRetention is what Plan.Retention starts from: the plan caps every
// metric kind and sets log retention outright. Raw core outlives the agent's 72 h replay window, so a replayed
// batch never lands in already-pruned time.
var DefaultRetention = Retention{
	DataRawCore:   7 * day,
	DataDetail:    7 * day,
	DataProcesses: 3 * day,
	DataRollup1m:  30 * day,
	DataRollup1h:  400 * day,
	DataLogs:      14 * day,
	// Raw check results back the response-time charts; the hourly rollup
	// and incidents back uptime and history, so they live as long as the
	// metric history does.
	DataMonitorResults:   30 * day,
	DataMonitorHourly:    400 * day,
	DataMonitorIncidents: 400 * day,
}
