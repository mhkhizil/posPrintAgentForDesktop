//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// ErrNotRunningAsService means --service was used by a process the SCM did not
// start, so the caller should fall back to running in the foreground.
var ErrNotRunningAsService = errors.New("the process was not started by the Windows service manager")

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procStartServiceCtrlDispatcherW   = advapi32.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerExW = advapi32.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatus              = advapi32.NewProc("SetServiceStatus")
)

const (
	serviceWin32OwnProcess = 0x00000010

	stateStopped      = 0x00000001
	stateStartPending = 0x00000002
	stateStopPending  = 0x00000003
	stateRunning      = 0x00000004

	acceptStop     = 0x00000001
	acceptShutdown = 0x00000004

	controlStop        = 0x00000001
	controlInterrogate = 0x00000004
	controlShutdown    = 0x00000005

	errorFailedControllerConnect = syscall.Errno(1063)
)

type serviceStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type serviceTableEntry struct {
	ServiceName *uint16
	ServiceProc uintptr
}

var (
	statusHandle   syscall.Handle
	currentState   = stateStopped
	changeRequests = make(chan uint32, 1)
	serviceRunner  Runner
	serviceExit    uint32
)

func winErr(err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return nil
	}
	return err
}

func reportStatus(state, accepted uint32) {
	currentState = state
	if statusHandle == 0 {
		return
	}
	status := serviceStatus{
		ServiceType:      serviceWin32OwnProcess,
		CurrentState:     state,
		ControlsAccepted: accepted,
	}
	procSetServiceStatus.Call(uintptr(statusHandle), uintptr(unsafe.Pointer(&status)))
	runtime.KeepAlive(status)
}

func handlerCallback(control, eventType uint32, eventData, context uintptr) uintptr {
	switch control {
	case controlInterrogate:
		reportStatus(currentState, acceptStop|acceptShutdown)
	case controlStop, controlShutdown:
		reportStatus(stateStopPending, 0)
		select {
		case changeRequests <- control:
		default:
		}
	}
	return 0
}

func mainCallback(argc uint32, argv **uint16) uintptr {
	namePtr, err := syscall.UTF16PtrFromString(Name)
	if err != nil {
		return 1
	}

	handle, _, _ := procRegisterServiceCtrlHandlerExW.Call(
		uintptr(unsafe.Pointer(namePtr)),
		syscall.NewCallback(handlerCallback),
		0,
	)
	runtime.KeepAlive(namePtr)
	if handle == 0 {
		return 1
	}
	statusHandle = syscall.Handle(handle)

	reportStatus(stateStartPending, 0)
	reportStatus(stateRunning, acceptStop|acceptShutdown)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serviceRunner(ctx) }()

	select {
	case runErr := <-done:
		if runErr != nil {
			serviceExit = 1
		}
	case <-changeRequests:
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	}

	reportStatus(stateStopped, 0)
	return uintptr(serviceExit)
}

// Run hands control to the SCM and blocks until the service stops.
func Run(run Runner) error {
	serviceRunner = run
	changeRequests = make(chan uint32, 1)

	namePtr, err := syscall.UTF16PtrFromString(Name)
	if err != nil {
		return err
	}

	entries := make([]serviceTableEntry, 2)
	entries[0] = serviceTableEntry{
		ServiceName: namePtr,
		ServiceProc: syscall.NewCallback(mainCallback),
	}
	entries[1] = serviceTableEntry{}

	// StartServiceCtrlDispatcherW must be called from the thread the SCM will
	// use for the service main function.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ok, _, callErr := procStartServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&entries[0])))
	runtime.KeepAlive(entries)
	runtime.KeepAlive(namePtr)
	if ok == 0 {
		err := winErr(callErr)
		if errors.Is(err, errorFailedControllerConnect) {
			return ErrNotRunningAsService
		}
		return fmt.Errorf("cannot start the service dispatcher: %w", err)
	}
	return nil
}
