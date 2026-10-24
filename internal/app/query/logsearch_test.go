package query

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func TestParseSearch(t *testing.T) {
	cases := []struct {
		in   string
		want []Clause
	}{
		{"", nil},
		{"timeout", []Clause{{"timeout"}}},
		{"timeout upstream", []Clause{{"timeout"}, {"upstream"}}},
		{"refused OR timeout dbx", []Clause{{"refused", "timeout"}, {"dbx"}}},
		{`"connection reset" peer`, []Clause{{"connection reset"}, {"peer"}}},
		// FTS5 operators and column filters are just text here.
		{`NEAR(abc title:xyz "a OR b"`, []Clause{{"NEAR(abc"}, {"title:xyz"}, {"a OR b"}}},
		// Only an exact, unquoted OR is the operator.
		{"error ORR warn", []Clause{{"error"}, {"ORR"}, {"warn"}}},
	}
	for _, c := range cases {
		got, err := ParseSearch(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseSearch(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}

func TestParseSearchRefuses(t *testing.T) {
	for _, in := range []string{"ab", "timeout ab", "OR timeout", "timeout OR", "a OR OR b", `"unterminated`,
		"aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk"} {
		if _, err := ParseSearch(in); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ParseSearch(%q) err = %v, want ErrInvalid", in, err)
		}
	}
}

func TestMatchesIsCaseInsensitiveSubstringAndOr(t *testing.T) {
	c, _ := ParseSearch(`refused OR TIMEOUT "port 5432"`)
	if !Matches(c, "Connection Refused on port 5432") || !Matches(c, "read timeout, port 5432") {
		t.Fatal("expected matches")
	}
	if Matches(c, "connection refused on port 6379") {
		t.Fatal("AND clause ignored")
	}
}

func TestCursorRoundTripAndValidation(t *testing.T) {
	c := LogCursor{TS: time.UnixMilli(1_790_000_000_123).UTC(), ID: 42}
	back, err := DecodeCursor(EncodeCursor(c))
	if err != nil || back != c {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	for _, bad := range []string{"!!", "bm9kb3Q", EncodeCursor(LogCursor{ID: 0}), "MTIzLmFiYw"} {
		if _, err := DecodeCursor(bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("DecodeCursor(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}
