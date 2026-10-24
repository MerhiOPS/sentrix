package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/infra/collector"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// Enroll exchanges a token for a credential and saves the identity in dir.
// The token's pin is applied to the TLS connection *before* the token secret
// is sent, so a man in the middle never sees it (PLAN §2).
func Enroll(ctx context.Context, dir, serverURL, token string, interval time.Duration) (Identity, error) {
	if _, err := LoadIdentity(dir); err == nil {
		return Identity{}, errors.New("already enrolled: remove the state directory to enroll again")
	}
	tok, err := wire.ParseToken(token)
	if err != nil {
		return Identity{}, err
	}
	serverURL = strings.TrimRight(serverURL, "/")
	client, err := NewClient(serverURL, tok.Pin)
	if err != nil {
		return Identity{}, err
	}
	hi := collector.ReadHostInfo(ctx)
	body, err := json.Marshal(wire.EnrollRequest{Token: token, Host: wire.HostInfo{
		Hostname: hi.Hostname, OS: hi.OS, Platform: hi.Platform,
		AgentVersion: Version, IntervalMS: interval.Milliseconds(),
	}})
	if err != nil {
		return Identity{}, fmt.Errorf("encode enroll request: %w", err)
	}
	resp, err := post(ctx, client, serverURL+wire.PathEnroll, "", body, false)
	if err != nil {
		return Identity{}, fmt.Errorf("enroll: %w", err)
	}
	if resp.Status != http.StatusOK {
		return Identity{}, fmt.Errorf("enroll refused (%d): %s", resp.Status, serverMessage(resp.Body))
	}
	var er wire.EnrollResponse
	if err := json.Unmarshal(resp.Body, &er); err != nil || er.Credential == "" {
		return Identity{}, fmt.Errorf("enroll: malformed response")
	}
	id := Identity{ServerURL: serverURL, Credential: er.Credential, HostID: er.HostID}
	if tok.Pin != nil {
		id.PinHex = hex.EncodeToString(tok.Pin)
	}
	if err := SaveIdentity(dir, id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// serverMessage extracts the error text from a wire.Error body.
func serverMessage(body []byte) string {
	var e wire.Error
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.TrimSpace(string(body))
}
