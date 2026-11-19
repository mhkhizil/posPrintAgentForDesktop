//go:build windows

package service

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	procOpenSCManagerW     = advapi32.NewProc("OpenSCManagerW")
	procCreateServiceW     = advapi32.NewProc("CreateServiceW")
	procOpenServiceW       = advapi32.NewProc("OpenServiceW")
	procStartServiceW      = advapi32.NewProc("StartServiceW")
	procControlService     = advapi32.NewProc("ControlService")
	procDeleteService      = advapi32.NewProc("DeleteService")
	procCloseServiceHandle = advapi32.NewProc("CloseServiceHandle")
)

const (
	scManagerCreateService = 0x0002
	deleteAccess           = 0x10000
	serviceAllAccess       = 0xF01FF
	serviceStartAccess     = 0x0010
	serviceStopAccess      = 0x0020
	serviceAutoStart       = 0x00000002
	serviceErrorNormal     = 0x00000001

	errorServiceDoesNotExist   = syscall.Errno(1060)
	errorServiceExists         = syscall.Errno(1073)
	errorServiceAlreadyRunning = syscall.Errno(1056)
)

func openSCManager() (syscall.Handle, error) {
	handle, _, callErr := procOpenSCManagerW.Call(0, 0, scManagerCreateService)
	if handle == 0 {
		return 0, fmt.Errorf("cannot open the service manager (run this from an elevated prompt): %w", winErr(callErr))
	}
	return syscall.Handle(handle), nil
}

// Install creates the service running as LocalSystem (the SCM default when no
// account is given), which is what lets it administer print queues, and starts it.
func Install(exePath, configPath string) error {
	scm, err := openSCManager()
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(scm))

	namePtr, _ := syscall.UTF16PtrFromString(Name)
	displayPtr, _ := syscall.UTF16PtrFromString(DisplayName)
	binaryPtr, _ := syscall.UTF16PtrFromString(fmt.Sprintf(`"%s" --service --config "%s"`, exePath, configPath))

	handle, _, callErr := procCreateServiceW.Call(
		uintptr(scm),
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(displayPtr)),
		serviceAllAccess,
		serviceWin32OwnProcess,
		serviceAutoStart,
		serviceErrorNormal,
		uintptr(unsafe.Pointer(binaryPtr)),
		0, 0, 0, 0, 0,
	)
	if handle == 0 {
		err := winErr(callErr)
		if errors.Is(err, errorServiceExists) {
			return startInstalled(scm)
		}
		return fmt.Errorf("cannot create the %s service: %w", Name, err)
	}

	installed := syscall.Handle(handle)
	defer procCloseServiceHandle.Call(uintptr(installed))

	// sc.exe is part of Windows and takes a documented command line, so the
	// recovery policy can be set without re-declaring the config structs here.
	_ = exec.Command("sc.exe", "failure", Name, "reset=", "86400",
		"actions=", "restart/5000/restart/5000/none/0").Run()

	startErr := startServiceHandle(installed)
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(displayPtr)
	runtime.KeepAlive(binaryPtr)
	return startErr
}

func startInstalled(scm syscall.Handle) error {
	namePtr, _ := syscall.UTF16PtrFromString(Name)
	handle, _, callErr := procOpenServiceW.Call(
		uintptr(scm),
		uintptr(unsafe.Pointer(namePtr)),
		serviceStartAccess|serviceStopAccess,
	)
	if handle == 0 {
		return fmt.Errorf("cannot open the %s service: %w", Name, winErr(callErr))
	}
	defer procCloseServiceHandle.Call(uintptr(handle))
	return startServiceHandle(syscall.Handle(handle))
}

func startServiceHandle(service syscall.Handle) error {
	ok, _, callErr := procStartServiceW.Call(uintptr(service), 0, 0)
	if ok == 0 {
		err := winErr(callErr)
		if errors.Is(err, errorServiceAlreadyRunning) {
			return nil
		}
		return fmt.Errorf("the service was installed but did not start: %w", err)
	}
	return nil
}

// Uninstall stops and deletes the service. It is not an error if it is absent.
func Uninstall() error {
	scm, err := openSCManager()
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(scm))

	namePtr, _ := syscall.UTF16PtrFromString(Name)
	handle, _, openErr := procOpenServiceW.Call(
		uintptr(scm),
		uintptr(unsafe.Pointer(namePtr)),
		serviceStopAccess|deleteAccess,
	)
	if handle == 0 {
		if errors.Is(winErr(openErr), errorServiceDoesNotExist) {
			return nil
		}
		return fmt.Errorf("cannot open the %s service: %w", Name, winErr(openErr))
	}

	service := syscall.Handle(handle)
	defer procCloseServiceHandle.Call(uintptr(service))

	var status serviceStatus
	procControlService.Call(uintptr(service), controlStop, uintptr(unsafe.Pointer(&status)))

	ok, _, callErr := procDeleteService.Call(uintptr(service))
	runtime.KeepAlive(namePtr)
	if ok == 0 {
		return fmt.Errorf("cannot remove the %s service: %w", Name, winErr(callErr))
	}
	return nil
}
