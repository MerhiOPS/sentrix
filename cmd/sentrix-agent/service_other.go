//go:build !windows

package main

import "errors"

var errNotWindows = errors.New("install/uninstall are for Windows; on Linux use the packaged systemd unit (sentrix-agent.service)")

func inService() bool           { return false }
func runService([]string) error { return errNotWindows }
func install([]string) error    { return errNotWindows }
func uninstall() error          { return errNotWindows }
