package logtail

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// splitter turns appended bytes into complete lines. A trailing partial line
// is held until its newline arrives, except that a line longer than the
// message limit is cut rather than held forever.
type splitter struct {
	partial []byte
}

// feed appends data and returns complete lines (without the newline).
func (s *splitter) feed(data []byte) []string {
	buf := append(s.partial, data...)
	var out []string
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		out = append(out, cleanLine(buf[:i]))
		buf = buf[i+1:]
	}
	if len(buf) > domain.MaxMessageBytes {
		out = append(out, cleanLine(buf[:domain.MaxMessageBytes]))
		buf = buf[domain.MaxMessageBytes:]
	}
	s.partial = append([]byte(nil), buf...)
	return out
}

// pending reports bytes held as a partial line.
func (s *splitter) pending() int { return len(s.partial) }

// cleanLine drops a trailing CR and replaces invalid UTF-8, so a binary or
// CRLF file still produces valid, searchable text.
func cleanLine(b []byte) string {
	b = bytes.TrimSuffix(b, []byte("\r"))
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// joiner merges continuation lines (starting with a space or tab — stack
// traces, multi-line JSON) into the line before them, capped at the message
// limit.
type joiner struct {
	open *string
}

// add takes one line and returns any completed entries.
func (j *joiner) add(line string) []string {
	if j.open != nil && isContinuation(line) {
		if len(*j.open)+1+len(line) <= domain.MaxMessageBytes {
			joined := *j.open + "\n" + line
			j.open = &joined
		}
		return nil
	}
	var done []string
	if j.open != nil {
		done = append(done, *j.open)
	}
	l := line
	j.open = &l
	return done
}

// flush returns the open entry, if any. Called when a poll finds no more
// data, so the last line of a quiet file is not held back indefinitely.
func (j *joiner) flush() []string {
	if j.open == nil {
		return nil
	}
	out := []string{*j.open}
	j.open = nil
	return out
}

// levelWords maps level tokens found near the start of a line.
var levelWords = []struct {
	word  string
	level domain.Level
}{
	{"FATAL", domain.LevelFatal}, {"PANIC", domain.LevelFatal}, {"CRIT", domain.LevelFatal},
	{"ERROR", domain.LevelError}, {"ERR", domain.LevelError},
	{"WARN", domain.LevelWarn},
	{"INFO", domain.LevelInfo}, {"NOTICE", domain.LevelInfo},
	{"DEBUG", domain.LevelDebug}, {"TRACE", domain.LevelDebug},
}

// guessLevel finds a level word in the first 80 bytes of a file line, as a
// whole word in any case. No word: unknown — never guessed from content.
func guessLevel(line string) domain.Level {
	head := strings.ToUpper(line[:min(len(line), 80)])
	for _, lw := range levelWords {
		for i := strings.Index(head, lw.word); i >= 0; {
			end := i + len(lw.word)
			if (i == 0 || !isWordByte(head[i-1])) && (end == len(head) || !isWordByte(head[end])) {
				return lw.level
			}
			next := strings.Index(head[i+1:], lw.word)
			if next < 0 {
				break
			}
			i += 1 + next
		}
	}
	return domain.LevelUnknown
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isContinuation(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}
