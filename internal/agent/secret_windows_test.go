//go:build windows

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDPAPIRoundTripAndIdentityProtection(t *testing.T) {
	dir := t.TempDir()
	const cred = "sentrix-test-credential-value"
	if err := SaveIdentity(dir, Identity{ServerURL: "https://s.example:8443", Credential: cred, HostID: "h"}); err != nil {
		t.Fatal(err)
	}
	changed, err := ProtectIdentity(dir)
	if err != nil || !changed {
		t.Fatalf("protect: changed %v, %v", changed, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, identityFile))
	if strings.Contains(string(raw), cred) || !strings.Contains(string(raw), "credential_protected") {
		t.Fatalf("the credential is still in plain text on disk: %s", raw)
	}
	id, err := LoadIdentity(dir)
	if err != nil || id.Credential != cred {
		t.Fatalf("load after protect: %q, %v", id.Credential, err)
	}
	if again, err := ProtectIdentity(dir); err != nil || again {
		t.Fatalf("second protect: changed %v, %v; want a no-op", again, err)
	}
}

func TestRestrictToServiceSetsProtectedACL(t *testing.T) {
	dir := t.TempDir()
	if err := RestrictToService(dir, "S-1-5-80-0"); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	sddl := sd.String()
	for _, want := range []string{"D:P", ";;;SY)", ";;;BA)", ";;;S-1-5-80-0)"} {
		if !strings.Contains(sddl, want) {
			t.Fatalf("ACL %s lacks %s", sddl, want)
		}
	}
	if strings.Contains(sddl, ";;;BU)") || strings.Contains(sddl, ";;;WD)") || strings.Contains(sddl, ";;;AU)") {
		t.Fatalf("ACL %s still grants ordinary users", sddl)
	}
}
