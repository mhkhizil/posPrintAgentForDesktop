//go:build windows

package printers

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetCommState     = kernel32.NewProc("GetCommState")
	procSetCommState     = kernel32.NewProc("SetCommState")
	procSetCommTimeouts  = kernel32.NewProc("SetCommTimeouts")
)

// DCB from winbase.h. The bitfield is set through Flags: fBinary | DTR enable |
// RTS enable, which is 8N1 with no flow control — what ESC/POS printers expect.
type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32
	wReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   byte
	Parity     byte
	StopBits   byte
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EofChar    byte
	EvtChar    byte
	wReserved1 uint16
}

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

const (
	dcbFlags8N1 = 0x00001021 // fBinary | DTR_CONTROL_ENABLE | RTS_CONTROL_ENABLE
	defaultBaud = 9600
)

// printSerial writes raw ESC/POS bytes to a Bluetooth SPP printer, which Windows
// exposes as an outgoing COM port once the device is paired.
func printSerial(port string, data []byte, timeout time.Duration) (int, error) {
	if strings.TrimSpace(port) == "" {
		return 0, &Error{Code: "PRINTER_REQUIRED", Message: "Choose the Bluetooth printer's COM port first."}
	}
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	path := port
	if !strings.HasPrefix(strings.ToUpper(path), `\\.\`) {
		path = `\\.\` + port
	}
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, &Error{Code: "PRINTER_REQUIRED", Message: fmt.Sprintf("The port %q cannot be used.", port)}
	}

	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return 0, &Error{
			Code: "PRINTER_NOT_FOUND",
			Message: fmt.Sprintf(
				"The Bluetooth printer port %s could not be opened: %v. Pair the printer in Windows > Bluetooth & devices > Printers & scanners, then tap Discover.",
				port, err,
			),
		}
	}
	defer syscall.CloseHandle(handle)

	if configErr := configureSerial(handle, timeout); configErr != nil {
		return 0, configErr
	}

	var written uint32
	if writeErr := syscall.WriteFile(handle, data, &written, nil); writeErr != nil {
		return int(written), &Error{
			Code:    "PRINTER_ERROR",
			Message: fmt.Sprintf("The Bluetooth printer on %s stopped accepting data: %v", port, writeErr),
		}
	}
	_ = syscall.FlushFileBuffers(handle)
	return int(written), nil
}

func configureSerial(handle syscall.Handle, timeout time.Duration) error {
	var state dcb
	state.DCBlength = uint32(unsafe.Sizeof(state))
	if ok, _, callErr := procGetCommState.Call(uintptr(handle), uintptr(unsafe.Pointer(&state))); ok == 0 {
		return serialConfigError(winErr(callErr))
	}

	state.BaudRate = defaultBaud
	state.Flags = dcbFlags8N1
	state.ByteSize = 8
	state.Parity = 0   // NOPARITY
	state.StopBits = 0 // ONESTOPBIT

	if ok, _, callErr := procSetCommState.Call(uintptr(handle), uintptr(unsafe.Pointer(&state))); ok == 0 {
		runtime.KeepAlive(&state)
		return serialConfigError(winErr(callErr))
	}
	// See enumPrinters: a pointer that reaches Win32 as a uintptr is invisible to
	// the collector, so it must be kept alive across the call.
	runtime.KeepAlive(&state)

	timeouts := commTimeouts{
		WriteTotalTimeoutConstant: uint32(timeout / time.Millisecond),
	}
	if ok, _, callErr := procSetCommTimeouts.Call(uintptr(handle), uintptr(unsafe.Pointer(&timeouts))); ok == 0 {
		runtime.KeepAlive(&timeouts)
		return serialConfigError(winErr(callErr))
	}
	runtime.KeepAlive(&timeouts)
	return nil
}

func serialConfigError(err error) error {
	if err == nil {
		err = fmt.Errorf("the port cannot be configured for a printer")
	}
	return &Error{
		Code:    "PRINTER_ERROR",
		Message: fmt.Sprintf("Windows could not configure the Bluetooth printer port: %v", err),
	}
}

// ListSerial returns the Bluetooth SPP COM ports. Windows registers them as
// \Device\BthModem0 under HARDWARE\DEVICEMAP\SERIALCOMM.
func ListSerial() ([]Printer, error) {
	values, err := regEnumValues(`HARDWARE\DEVICEMAP\SERIALCOMM`)
	if err != nil {
		return []Printer{}, nil
	}

	list := make([]Printer, 0, len(values))
	for _, value := range values {
		lower := strings.ToLower(value.Name)
		if !strings.Contains(lower, "bth") && !strings.Contains(lower, "bluetooth") {
			continue
		}
		port := strings.TrimSpace(value.Value)
		if port == "" {
			continue
		}
		list = append(list, Printer{
			ID:        "serial:" + strings.ToLower(port),
			Kind:      "BLUETOOTH",
			Name:      port,
			Port:      port,
			Model:     strings.TrimPrefix(value.Name, `\Device\`),
			Connected: true,
			Status:    "idle",
		})
	}
	return list, nil
}
