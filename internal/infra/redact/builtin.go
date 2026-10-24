// Package redact removes secrets and personal data from log lines at
// ingest, before storage and before the search index (PLAN Phase 4): data
// that is never stored needs no erasure.
package redact

import (
	"net"
	"regexp"
	"strings"
)

// builtin is one always-on rule. A validator, when set, must accept the
// match too — the regex finds candidates, the validator decides, so an
// IPv6-shaped timestamp is not mangled (OLD C-M10).
type builtin struct {
	name  string
	re    *regexp.Regexp
	repl  string
	valid func(string) bool
}

// Placeholders. Fixed strings so a search for them finds redacted lines.
const (
	maskSecret = "[REDACTED]"
	maskEmail  = "[EMAIL]"
	maskIP     = "[IP]"
)

var builtins = []builtin{
	{name: "bearer", re: regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9\-._~+/]{8,}=*`), repl: "${1} " + maskSecret},
	{name: "authorization header", re: regexp.MustCompile(`(?i)(\bauthorization\s*[:=]\s*)\S.*`), repl: "${1}" + maskSecret},
	{name: "url credentials", re: regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^\s:/@]+:)[^\s@/]+@`), repl: "${1}" + maskSecret + "@"},
	{name: "key=value secret", re: regexp.MustCompile(
		`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`),
		repl: "${1}${2}" + maskSecret},
	{name: "email", re: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), repl: maskEmail},
	{name: "ipv4", re: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), repl: maskIP, valid: isIPv4},
	{name: "ipv6", re: regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?:%[0-9a-z]+)?`), repl: maskIP, valid: isIPv6},
}

func isIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// isIPv6 accepts only what net.ParseIP accepts as IPv6: "12:34:56" (a
// time) and "aa:bb:cc:dd:ee:ff" (a MAC) are not addresses.
func isIPv6(s string) bool {
	host, _, _ := strings.Cut(s, "%") // zone index
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil && strings.Contains(host, ":")
}

// applyBuiltins runs every built-in rule over s.
func applyBuiltins(s string) string {
	for _, b := range builtins {
		if b.valid == nil {
			s = b.re.ReplaceAllString(s, b.repl)
			continue
		}
		s = b.re.ReplaceAllStringFunc(s, func(m string) string {
			if b.valid(m) {
				return b.repl
			}
			return m
		})
	}
	return s
}
