//go:build !windows

package main

// defaultStateDir holds the identity and batch_seq; the systemd unit
// (Phase 6) creates it owned by the agent's user.
const defaultStateDir = "/var/lib/sentrix-agent"
