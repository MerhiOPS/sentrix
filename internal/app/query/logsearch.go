package query

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Search semantics (PLAN §4.2), identical in every adapter:
//   - each term is a case-insensitive substring of the message;
//   - whitespace between terms is AND; an uppercase OR joins its neighbours;
//   - "a quoted phrase" is one term, spaces included;
//   - every term is at least MinTermLen characters — shorter is refused,
//     never silently turned into a full scan;
//   - no stemming, no ranking; results are newest first.
//
// Nothing in a term is syntax to the store: adapters must treat it as
// literal text (SQLite quotes each term for FTS5).
const (
	MinTermLen  = 3
	MaxTerms    = 10
	maxQueryLen = 1000
)

// Clause is a set of terms of which at least one must match (OR).
type Clause []string

// ParseSearch turns user input into AND-ed clauses. Empty input means no
// text filter.
func ParseSearch(q string) ([]Clause, error) {
	if len(q) > maxQueryLen {
		return nil, fmt.Errorf("%w: query longer than %d bytes", domain.ErrInvalid, maxQueryLen)
	}
	toks, err := tokenize(q)
	if err != nil {
		return nil, err
	}
	var out []Clause
	joinNext := false
	terms := 0
	for _, t := range toks {
		if !t.quoted && t.text == "OR" {
			if len(out) == 0 || joinNext {
				return nil, fmt.Errorf("%w: OR needs a term on each side", domain.ErrInvalid)
			}
			joinNext = true
			continue
		}
		if n := utf8.RuneCountInString(t.text); n < MinTermLen {
			return nil, fmt.Errorf("%w: search term %q is shorter than %d characters", domain.ErrInvalid, t.text, MinTermLen)
		}
		if terms++; terms > MaxTerms {
			return nil, fmt.Errorf("%w: more than %d search terms", domain.ErrInvalid, MaxTerms)
		}
		if joinNext {
			out[len(out)-1] = append(out[len(out)-1], t.text)
			joinNext = false
			continue
		}
		out = append(out, Clause{t.text})
	}
	if joinNext {
		return nil, fmt.Errorf("%w: OR needs a term on each side", domain.ErrInvalid)
	}
	return out, nil
}

type token struct {
	text   string
	quoted bool
}

func tokenize(q string) ([]token, error) {
	var out []token
	for i := 0; i < len(q); {
		switch q[i] {
		case ' ', '\t', '\n':
			i++
		case '"':
			end := strings.IndexByte(q[i+1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("%w: unterminated quote", domain.ErrInvalid)
			}
			out = append(out, token{text: q[i+1 : i+1+end], quoted: true})
			i += end + 2
		default:
			j := i
			for j < len(q) && q[j] != ' ' && q[j] != '\t' && q[j] != '\n' && q[j] != '"' {
				j++
			}
			out = append(out, token{text: q[i:j]})
			i = j
		}
	}
	return out, nil
}

// Matches applies the semantics to one message: the reference the memstore
// adapter uses, and the definition the contract suite pins SQLite to.
func Matches(clauses []Clause, message string) bool {
	m := strings.ToLower(message)
	for _, c := range clauses {
		ok := false
		for _, term := range c {
			if strings.Contains(m, strings.ToLower(term)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
