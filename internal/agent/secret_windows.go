//go:build windows

package agent

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protectionAvailable: DPAPI is available on Windows.
const protectionAvailable = true

// protectSecret encrypts b with DPAPI in the calling account's scope (the
// service's virtual account when run as the service): only that account on
// this machine can decrypt it, so a copied state directory is useless.
func protectSecret(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("dpapi: empty secret")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]} //nolint:gosec // a credential is far below 4 GiB
	var out windows.DataBlob
	desc, _ := windows.UTF16PtrFromString("sentrix-agent credential")
	if err := windows.CryptProtectData(&in, desc, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("dpapi protect: %w", err)
	}
	return takeBlob(out), nil
}

// unprotectSecret reverses protectSecret.
func unprotectSecret(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("dpapi: empty blob")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]} //nolint:gosec // bounded by the identity file size
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("dpapi unprotect (was the credential protected by another account?): %w", err)
	}
	return takeBlob(out), nil
}

// takeBlob copies a DPAPI output blob into Go memory and frees it. The
// unsafe calls are the documented way to read and free memory DPAPI
// allocated: Data points at Size bytes from LocalAlloc.
func takeBlob(b windows.DataBlob) []byte {
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data))) }() //nolint:gosec // see above
	return append([]byte(nil), unsafe.Slice(b.Data, b.Size)...)                         //nolint:gosec // see above
}

// RestrictToService replaces path's ACL with full control for SYSTEM,
// Administrators and the given account SID (the service's virtual account),
// and nothing else — inherited entries included, so files created inside
// later carry the same ACL.
func RestrictToService(path, accountSID string) error {
	sddl := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	if accountSID != "" {
		sddl += "(A;OICI;FA;;;" + accountSID + ")"
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build acl: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read acl: %w", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("set acl on %s: %w", path, err)
	}
	return nil
}
