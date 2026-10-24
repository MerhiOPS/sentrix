package domain

import (
	"fmt"
	"strings"
	"time"
)

// Level is a log line's severity, normalised across sources.
type Level string

const (
	LevelUnknown Level = "unknown"
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarn    Level = "warn"
	LevelError   Level = "error"
	LevelFatal   Level = "fatal"
)

// levelAliases maps the spellings seen in syslog, journald, Windows Event
// Log and common application loggers onto Level.
var levelAliases = map[string]Level{
	"debug": LevelDebug, "trace": LevelDebug, "verbose": LevelDebug, "7": LevelDebug,
	"info": LevelInfo, "information": LevelInfo, "notice": LevelInfo, "6": LevelInfo, "5": LevelInfo,
	"warn": LevelWarn, "warning": LevelWarn, "4": LevelWarn,
	"error": LevelError, "err": LevelError, "3": LevelError,
	"fatal": LevelFatal, "critical": LevelFatal, "crit": LevelFatal, "alert": LevelFatal,
	"emerg": LevelFatal, "emergency": LevelFatal, "panic": LevelFatal,
	"0": LevelFatal, "1": LevelFatal, "2": LevelFatal,
}

// validLevels is the closed set a stored line may carry.
var validLevels = map[Level]bool{
	LevelUnknown: true, LevelDebug: true, LevelInfo: true,
	LevelWarn: true, LevelError: true, LevelFatal: true,
}

// ParseLevel normalises a level string. It never guesses: anything it does
// not recognise is LevelUnknown.
func ParseLevel(s string) Level {
	if l, ok := levelAliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return l
	}
	return LevelUnknown
}

// Log line limits. A line longer than MaxMessageBytes is truncated by the
// agent, never silently by storage.
const (
	MaxMessageBytes = 16 * 1024
	MaxSourceBytes  = 512
)

// LogLine is one log entry from one host.
type LogLine struct {
	TS      time.Time
	Source  string // file path, journald unit, or Windows channel
	Level   Level
	Message string
	Labels  Labels
}

// Validate rejects lines storage must not accept.
func (l LogLine) Validate() error {
	if l.TS.IsZero() {
		return fmt.Errorf("%w: log line has no timestamp", ErrInvalid)
	}
	if len(l.Message) > MaxMessageBytes {
		return fmt.Errorf("%w: log message %d bytes, max %d", ErrInvalid, len(l.Message), MaxMessageBytes)
	}
	if len(l.Source) > MaxSourceBytes {
		return fmt.Errorf("%w: log source too long", ErrInvalid)
	}
	if !validLevels[l.Level] {
		return fmt.Errorf("%w: log level %q is not normalised", ErrInvalid, l.Level)
	}
	return l.Labels.Validate()
}

// Size is the line's billable size in bytes: what the log quota counts
// (PLAN Phase 4: quotas are bytes, never lines).
func (l LogLine) Size() int {
	n := len(l.Message) + len(l.Source)
	for k, v := range l.Labels {
		n += len(k) + len(v)
	}
	return n
}
