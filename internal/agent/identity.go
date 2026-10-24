package agent

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const identityFile = "identity.json"

// ErrNotEnrolled means the state directory holds no identity yet.
var ErrNotEnrolled = errors.New("agent is not enrolled: run `sentrix-agent enroll` first")

// Identity is what enrollment produces and every batch uses. It holds the
// credential, so it is written 0600 (Windows DPAPI protection is Phase 6).
type Identity struct {
	ServerURL  string `json:"server_url"`
	Credential string `json:"credential"`
	HostID     string `json:"host_id"`
	// PinHex is the server's SPKI pin. Empty means the server uses a
	// CA-issued certificate and system roots are used instead.
	PinHex string `json:"pin,omitempty"`
}

// Pin decodes PinHex; nil when the server is CA-verified.
func (id Identity) Pin() ([]byte, error) {
	if id.PinHex == "" {
		return nil, nil
	}
	p, err := hex.DecodeString(id.PinHex)
	if err != nil || len(p) != 32 {
		return nil, fmt.Errorf("identity has a malformed pin")
	}
	return p, nil
}

// SaveIdentity writes the identity atomically with owner-only permissions.
func SaveIdentity(dir string, id Identity) error {
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("encode identity: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, identityFile), b, 0o600)
}

// LoadIdentity reads the identity, or ErrNotEnrolled.
func LoadIdentity(dir string) (Identity, error) {
	b, err := os.ReadFile(filepath.Join(dir, identityFile)) //nolint:gosec // dir is the operator-configured state directory
	if errors.Is(err, os.ErrNotExist) {
		return Identity{}, ErrNotEnrolled
	}
	if err != nil {
		return Identity{}, fmt.Errorf("read identity: %w", err)
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return Identity{}, fmt.Errorf("decode identity: %w", err)
	}
	if id.ServerURL == "" || id.Credential == "" {
		return Identity{}, errors.New("identity is incomplete: re-enroll")
	}
	if _, err := id.Pin(); err != nil {
		return Identity{}, err
	}
	return id, nil
}
