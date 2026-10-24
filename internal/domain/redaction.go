package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// RedactionAction is what a matching rule does to a log line.
type RedactionAction string

const (
	// RedactMask replaces each match with the rule's replacement.
	RedactMask RedactionAction = "mask"
	// RedactDrop drops the whole line if any field matches.
	RedactDrop RedactionAction = "drop"
)

// RedactionRule is a tenant's own rule, applied after the built-in ones.
// Patterns are RE2 (Go regexp): linear-time, so a hostile pattern cannot
// make ingest backtrack exponentially.
type RedactionRule struct {
	ID          int64
	Pattern     string
	Replacement string
	Action      RedactionAction
}

// Rule limits: bounded count and size keep per-line cost bounded.
const (
	MaxRedactionRules       = 50
	MaxRedactionPatternLen  = 500
	MaxRedactionReplaceLen  = 100
	DefaultRedactionReplace = "[REDACTED]"
)

// CompileRule validates a rule and returns its compiled pattern.
func CompileRule(r RedactionRule) (*regexp.Regexp, error) {
	if r.Action != RedactMask && r.Action != RedactDrop {
		return nil, fmt.Errorf("%w: action must be mask or drop", ErrInvalid)
	}
	if strings.TrimSpace(r.Pattern) == "" || len(r.Pattern) > MaxRedactionPatternLen {
		return nil, fmt.Errorf("%w: pattern must be 1-%d characters", ErrInvalid, MaxRedactionPatternLen)
	}
	if len(r.Replacement) > MaxRedactionReplaceLen {
		return nil, fmt.Errorf("%w: replacement too long", ErrInvalid)
	}
	re, err := regexp.Compile(r.Pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: pattern: %w", ErrInvalid, err)
	}
	if re.MatchString("") {
		return nil, fmt.Errorf("%w: pattern matches the empty string", ErrInvalid)
	}
	return re, nil
}
