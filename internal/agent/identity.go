package agent

import (
	"encoding/base64"
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
// credential: written 0600, and on Windows DPAPI-encrypted under the service
// account the first time the service loads it (ProtectIdentity).
type Identity struct {
	ServerURL  string `json:"server_url"`
	Credential string `json:"credential,omitempty"`
	// Protected is the DPAPI-encrypted credential (base64); when set,
	// Credential is empty on disk.
	Protected string `json:"credential_protected,omitempty"`
	HostID    string `json:"host_id"`
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
	if id.Protected != "" {
		blob, err := base64.StdEncoding.DecodeString(id.Protected)
		if err != nil {
			return Identity{}, fmt.Errorf("decode protected credential: %w", err)
		}
		plain, err := unprotectSecret(blob)
		if err != nil {
			return Identity{}, err
		}
		id.Credential, id.Protected = string(plain), ""
	}
	if id.ServerURL == "" || id.Credential == "" {
		return Identity{}, errors.New("identity is incomplete: re-enroll")
	}
	if _, err := id.Pin(); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// ProtectIdentity rewrites a plaintext identity with the credential
// DPAPI-encrypted under the calling account. Only the service calls it, on
// its first start: protecting at enrollment would bind the credential to the
// administrator who ran enroll, and the service account could never read it.
// It reports whether it changed anything; on Unix it never does.
func ProtectIdentity(dir string) (bool, error) {
	if !protectionAvailable {
		return false, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, identityFile)) //nolint:gosec // dir is the operator-configured state directory
	if err != nil {
		return false, fmt.Errorf("read identity: %w", err)
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return false, fmt.Errorf("decode identity: %w", err)
	}
	if id.Protected != "" || id.Credential == "" {
		return false, nil
	}
	blob, err := protectSecret([]byte(id.Credential))
	if err != nil {
		return false, err
	}
	id.Protected, id.Credential = base64.StdEncoding.EncodeToString(blob), ""
	return true, SaveIdentity(dir, id)
}
