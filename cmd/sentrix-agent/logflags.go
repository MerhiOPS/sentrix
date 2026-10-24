package main

import (
	"flag"
	"strings"

	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
)

// listFlag is a repeatable string flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// logFlags selects log sources. Nothing is collected unless asked for: log
// content is the data most likely to hold secrets.
type logFlags struct {
	files    listFlag
	journald bool
	units    listFlag
	eventlog string
}

func (f *logFlags) register(fs *flag.FlagSet) {
	fs.Var(&f.files, "log-file", "tail files matching this glob (repeatable), e.g. /var/log/nginx/*.log")
	fs.BoolVar(&f.journald, "journald", false, "follow the systemd journal")
	fs.Var(&f.units, "journald-unit", "limit the journal to this unit (repeatable)")
	fs.StringVar(&f.eventlog, "eventlog", "", "Windows Event Log channels, comma-separated, e.g. Application,System")
}

// sources builds the configured log sources.
func (f *logFlags) sources() ([]logtail.Source, error) {
	var out []logtail.Source
	if len(f.files) > 0 {
		fs, err := logtail.NewFileSource("files", f.files)
		if err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	if f.journald {
		out = append(out, logtail.NewJournaldSource(f.units))
	}
	if f.eventlog != "" {
		var chans []string
		for _, c := range strings.Split(f.eventlog, ",") {
			if c = strings.TrimSpace(c); c != "" {
				chans = append(chans, c)
			}
		}
		out = append(out, logtail.NewEventLogSource(chans))
	}
	return out, nil
}
