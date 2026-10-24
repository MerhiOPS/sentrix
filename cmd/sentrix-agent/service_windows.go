//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/MerhiOPS/sentrix/internal/agent"
)

const (
	serviceName = "sentrix-agent"
	// serviceAccount is the service's virtual account: a per-service
	// identity with no password and no rights beyond what is granted to it,
	// instead of LocalSystem.
	serviceAccount = `NT SERVICE\` + serviceName
	stopTimeout    = 20 * time.Second
)

func inService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// runService runs `run` under the Service Control Manager. The SCM starts
// the binary with the arguments registered at install ("run --state-dir …").
func runService(args []string) error {
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	o, err := parseRun(args)
	if err != nil {
		return err
	}
	return svc.Run(serviceName, &handler{opts: o})
}

type handler struct{ opts runOptions }

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	logOut, err := openServiceLog(h.opts.dir)
	if err != nil {
		return true, 1
	}
	defer func() { _ = logOut.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runWith(ctx, h.opts, logOut, true) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done: // the agent stopped on its own: a startup error
			cancel()
			if err != nil {
				fmt.Fprintln(logOut, "agent stopped:", err)
				return true, 2
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil { // the agent seals and drains within its own timeout
					fmt.Fprintln(logOut, "agent stopped:", err)
				}
				return false, 0
			}
		}
	}
}

// openServiceLog appends to agent.log in the state dir, starting over when
// it passes 10 MiB (the previous file is kept as agent.log.1).
func openServiceLog(dir string) (*os.File, error) {
	path := filepath.Join(dir, "agent.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 10<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path is under the state dir
}

// install registers the service under its virtual account, locks the state
// directory down to SYSTEM, Administrators and that account, and starts it.
// The host must already be enrolled into the same state directory.
func install(args []string) error {
	o, err := parseRun(args)
	if err != nil {
		return err
	}
	if _, err := agent.LoadIdentity(o.dir); err != nil {
		return fmt.Errorf("enroll first (sentrix-agent enroll --state-dir %s …): %w", o.dir, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service manager (run as Administrator): %w", err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "Sentrix agent", Description: "Ships host metrics and logs to a Sentrix server.",
		StartType: mgr.StartAutomatic, ServiceStartName: serviceAccount,
	}, append([]string{"run"}, args...)...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer func() { _ = s.Close() }()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second}, {Type: mgr.ServiceRestart, Delay: time.Minute},
	}, uint32((24 * time.Hour).Seconds()))
	sid, err := serviceSID()
	if err != nil {
		return err
	}
	if err := agent.RestrictToService(o.dir, sid); err != nil {
		return err
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	fmt.Printf("service %s installed as %s and started; state in %s\n", serviceName, serviceAccount, o.dir)
	return nil
}

// serviceSID resolves the virtual account's SID. It exists once the
// service is created.
func serviceSID() (string, error) {
	sid, _, _, err := windows.LookupSID("", serviceAccount)
	if err != nil {
		return "", fmt.Errorf("look up %s: %w", serviceAccount, err)
	}
	return sid.String(), nil
}

// uninstall stops and removes the service. The state directory is left in
// place (it holds the credential and any unsent batches).
func uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service manager (run as Administrator): %w", err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer func() { _ = s.Close() }()
	if st, err := s.Control(svc.Stop); err == nil {
		deadline := time.Now().Add(stopTimeout)
		for st.State != svc.Stopped && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			if st, err = s.Query(); err != nil {
				break
			}
		}
	}
	if err := s.Delete(); err != nil {
		return errors.Join(errors.New("delete service"), err)
	}
	fmt.Println("service removed; state directory kept")
	return nil
}
