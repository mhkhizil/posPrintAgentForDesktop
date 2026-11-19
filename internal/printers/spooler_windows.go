//go:build windows

package printers

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// PRINTER_ALL_ACCESS from winspool.h: administer + use + standard rights.
const printerAllAccess = 0xF000C

type docInfo1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

type printerDefaults struct {
	pDatatype     *uint16
	pDevMode      uintptr
	DesiredAccess uint32
}

// Win32 spooler errors a shopkeeper can act on.
const (
	errInvalidPrinterName = syscall.Errno(1801)
	errUnknownPrinter     = syscall.Errno(1798)
	errPrinterBusy        = syscall.Errno(1805)
)

// printSpooler writes a RAW job to a Windows print queue. The printer's own
// driver owns the USB device, so the queue is the only path that works — and it
// is what survives a changed USB port or a printer being switched off and on.
func printSpooler(name string, data []byte) (int, error) {
	if name == "" {
		return 0, &Error{Code: "PRINTER_REQUIRED", Message: "Choose a printer first."}
	}

	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, &Error{Code: "PRINTER_REQUIRED", Message: fmt.Sprintf("The printer name %q cannot be used.", name)}
	}
	docNamePtr, err := syscall.UTF16PtrFromString("POS receipt")
	if err != nil {
		return 0, &Error{Code: "PRINTER_ERROR", Message: err.Error()}
	}
	rawPtr, err := syscall.UTF16PtrFromString("RAW")
	if err != nil {
		return 0, &Error{Code: "PRINTER_ERROR", Message: err.Error()}
	}

	var handle syscall.Handle
	ok, _, callErr := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&handle)),
		0,
	)
	if ok == 0 {
		return 0, spoolerError(winErr(callErr), name)
	}
	defer procClosePrinter.Call(uintptr(handle))

	doc := docInfo1{pDocName: docNamePtr, pDatatype: rawPtr}
	ok, _, callErr = procStartDocPrinterW.Call(
		uintptr(handle),
		1,
		uintptr(unsafe.Pointer(&doc)),
	)
	if ok == 0 {
		return 0, spoolerError(winErr(callErr), name)
	}
	defer procEndDocPrinter.Call(uintptr(handle))

	if ok, _, callErr = procStartPagePrinter.Call(uintptr(handle)); ok == 0 {
		return 0, spoolerError(winErr(callErr), name)
	}

	var written uint32
	dataPtr := uintptr(0)
	if len(data) > 0 {
		dataPtr = uintptr(unsafe.Pointer(&data[0]))
	}
	ok, _, callErr = procWritePrinter.Call(
		uintptr(handle),
		dataPtr,
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
	)
	if ok == 0 {
		return int(written), spoolerError(winErr(callErr), name)
	}

	if ok, _, callErr = procEndPagePrinter.Call(uintptr(handle)); ok == 0 {
		return int(written), spoolerError(winErr(callErr), name)
	}

	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(docNamePtr)
	runtime.KeepAlive(rawPtr)
	runtime.KeepAlive(doc)
	runtime.KeepAlive(data)

	return int(written), nil
}

// deletePrinter removes a print queue. The caller must already be elevated.
func deletePrinter(name string) error {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	var handle syscall.Handle
	defaults := printerDefaults{DesiredAccess: printerAllAccess}
	ok, _, callErr := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&handle)),
		uintptr(unsafe.Pointer(&defaults)),
	)
	if ok == 0 {
		return spoolerError(winErr(callErr), name)
	}
	defer procClosePrinter.Call(uintptr(handle))

	ok, _, callErr = procDeletePrinter.Call(uintptr(handle))
	if ok == 0 {
		return spoolerError(winErr(callErr), name)
	}
	runtime.KeepAlive(defaults)
	return nil
}

// spoolerError turns a Win32 error into the sentence the POS shows.
func spoolerError(err error, name string) error {
	switch {
	case errors.Is(err, errInvalidPrinterName), errors.Is(err, errUnknownPrinter):
		return &Error{
			Code: "PRINTER_NOT_FOUND",
			Message: fmt.Sprintf(
				"The Windows print queue %q is not installed on this computer. Install its driver, then open Settings > Printer and pick it again.",
				name,
			),
		}
	case errors.Is(err, syscall.ERROR_ACCESS_DENIED):
		return &Error{
			Code:    "PRINTER_ACCESS_DENIED",
			Message: fmt.Sprintf("Windows refused access to the printer %q. The print agent service may need to run as LocalSystem.", name),
		}
	case errors.Is(err, errPrinterBusy):
		return &Error{
			Code:    "PRINTER_BUSY",
			Message: fmt.Sprintf("The printer %q is busy. Wait a moment and print again.", name),
		}
	}

	if err == nil {
		return &Error{Code: "PRINTER_ERROR", Message: fmt.Sprintf("The printer %q could not print.", name)}
	}
	return &Error{Code: "PRINTER_ERROR", Message: fmt.Sprintf("The printer %q could not print: %v", name, err)}
}
