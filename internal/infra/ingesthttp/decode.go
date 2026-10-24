// Package ingesthttp is the agent-facing HTTP endpoint (/ingest/v1). It maps
// the wire protocol onto the ingest and identity use cases and owns the
// response contract: every status has exactly one meaning (PLAN §2).
package ingesthttp

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// errTooLarge marks a body over a size limit (→ 413, never retried).
var errTooLarge = errors.New("request body too large")

// errMalformed marks a body that is not valid JSON of the right shape
// (→ 400, never retried).
var errMalformed = errors.New("malformed request body")

// decodeBody reads a JSON body into v. The compressed size is capped by
// MaxBytesReader and the decompressed size separately, so a small gzip bomb
// cannot inflate into gigabytes. Unknown fields are accepted: the protocol
// changes additively, and a newer agent must work with an older server.
func decodeBody(w http.ResponseWriter, r *http.Request, maxCompressed, maxDecompressed int64, v any) error {
	body := http.MaxBytesReader(w, r.Body, maxCompressed)
	defer func() { _ = body.Close() }() // request body: read-only

	var src io.Reader = body
	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(body)
		if err != nil {
			return classify(err)
		}
		defer func() { _ = zr.Close() }() // checksum errors surface from ReadAll
		src = zr
	default:
		return fmt.Errorf("%w: unsupported Content-Encoding %q", errMalformed, enc)
	}
	limited := &io.LimitedReader{R: src, N: maxDecompressed + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return classify(err)
	}
	if int64(len(data)) > maxDecompressed {
		return fmt.Errorf("%w: decompressed body exceeds %d bytes", errTooLarge, maxDecompressed)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %w", errMalformed, err)
	}
	return nil
}

// classify turns read errors into the two client-error kinds.
func classify(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return fmt.Errorf("%w: body exceeds %d bytes", errTooLarge, mbe.Limit)
	}
	return fmt.Errorf("%w: %w", errMalformed, err)
}
