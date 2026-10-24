//go:build !windows

package agent

import "errors"

// protectionAvailable: on Unix the credential file is 0600 and its directory
// 0700, owned by the agent's dedicated user; there is no DPAPI equivalent.
const protectionAvailable = false

var errNoProtection = errors.New("credential encryption is Windows-only")

func protectSecret([]byte) ([]byte, error)   { return nil, errNoProtection }
func unprotectSecret([]byte) ([]byte, error) { return nil, errNoProtection }
