package wire

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func secret(b byte) []byte { return bytes.Repeat([]byte{b}, SecretLen) }

func TestTokenRoundTrip(t *testing.T) {
	for _, tok := range []Token{
		{Secret: secret(1), Pin: bytes.Repeat([]byte{0xab}, PinLen)},
		{Secret: secret(2)}, // CA mode: no pin
	} {
		back, err := ParseToken(tok.String())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(back.Secret, tok.Secret) || !bytes.Equal(back.Pin, tok.Pin) {
			t.Fatalf("round trip changed the token: %+v -> %+v", tok, back)
		}
	}
}

func TestParseTokenRejectsMalformed(t *testing.T) {
	good := Token{Secret: secret(3), Pin: bytes.Repeat([]byte{1}, PinLen)}.String()
	parts := strings.Split(good, ".")
	for name, s := range map[string]string{
		"empty":        "",
		"wrong prefix": "stx2." + parts[1],
		"short secret": "stx1." + EncodeSecret(secret(1)[:16]),
		"bad base64":   "stx1.!!!",
		"short pin":    parts[0] + "." + parts[1] + ".abcd",
		"non-hex pin":  parts[0] + "." + parts[1] + "." + strings.Repeat("zz", PinLen),
		"extra part":   good + ".x",
	} {
		if _, err := ParseToken(s); err == nil {
			t.Errorf("%s: %q accepted", name, s)
		}
	}
}

func TestDecodeSecretLength(t *testing.T) {
	if _, err := DecodeSecret(EncodeSecret(secret(9))); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSecret(EncodeSecret(secret(9)[:31])); err == nil {
		t.Fatal("31-byte credential accepted")
	}
}

// TestBatchJSONFieldNames pins the wire spelling: renaming a Go field must
// not silently change the protocol.
func TestBatchJSONFieldNames(t *testing.T) {
	b, err := json.Marshal(Batch{BatchSeq: 7, Samples: []Sample{{TSMS: 1, DiskWriteBytes: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"batch_seq":7`, `"ts_ms":1`, `"disk_w":2`, `"interval_ms"`, `"agent":{`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("batch JSON lacks %s: %s", want, b)
		}
	}
}
