//go:build !windows

package service

import "errors"

var (
	// ErrUnsupported is returned when service management is attempted off Windows.
	ErrUnsupported = errors.New("Windows services are only available on Windows; run the agent in the foreground")
	// ErrNotRunningAsService mirrors the Windows sentinel so callers need no build tags.
	ErrNotRunningAsService = errors.New("the process was not started by the Windows service manager")
)

// Run is not supported off Windows.
func Run(run Runner) error { return ErrUnsupported }

// Install is not supported off Windows.
func Install(exePath, configPath string) error { return ErrUnsupported }

// Uninstall is not supported off Windows.
func Uninstall() error { return ErrUnsupported }

