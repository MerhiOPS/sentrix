//go:build windows

package main

// defaultStateDir is under ProgramData so the service account can reach it.
const defaultStateDir = `C:\ProgramData\sentrix-agent`
