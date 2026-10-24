package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCompileRule(t *testing.T) {
	if _, err := CompileRule(RedactionRule{Pattern: `acct-\d+`, Action: RedactMask}); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]RedactionRule{
		"bad action":    {Pattern: "x", Action: "erase"},
		"empty":         {Pattern: "  ", Action: RedactMask},
		"too long":      {Pattern: strings.Repeat("a", MaxRedactionPatternLen+1), Action: RedactMask},
		"invalid RE2":   {Pattern: `(?<=x)y`, Action: RedactMask}, // lookbehind: not RE2
		"matches empty": {Pattern: `a*`, Action: RedactDrop},      // would mask between every character
		"long replace":  {Pattern: "x", Replacement: strings.Repeat("r", MaxRedactionReplaceLen+1), Action: RedactMask},
	} {
		if _, err := CompileRule(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
