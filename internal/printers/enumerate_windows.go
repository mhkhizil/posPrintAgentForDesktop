//go:build windows

package printers

import (
	"sort"
	"strings"
	"unsafe"
)

// PRINTER_ENUM_* and PRINTER_INFO_2 flags from winspool.h.
const (
	printerEnumLocal       = 0x00000002
	printerEnumConnections = 0x00000004

	printerAttributeWorkOffline = 0x00000400

	printerStatusError    = 0x00000002
	printerStatusOffline  = 0x00000080
	printerStatusBusy     = 0x00000200
	printerStatusPrinting = 0x00000400
)

// printerInfo2 mirrors PRINTER_INFO_2W. Pointer fields are pointer-sized, so the
// layout matches the Win32 struct on both 386 and amd64.
type printerInfo2 struct {
	pServerName         *uint16
	pPrinterName        *uint16
	pShareName          *uint16
	pPortName           *uint16
	pDriverName         *uint16
	pComment            *uint16
	pLocation           *uint16
	pDevMode            uintptr
	pSepFile            *uint16
	pPrintProcessor     *uint16
	pDatatype           *uint16
	pParameters         *uint16
	pSecurityDescriptor uintptr
	Attributes          uint32
	Priority            uint32
	DefaultPriority     uint32
	StartTime           uint32
	UntilTime           uint32
	Status              uint32
	cJobs               uint32
	AveragePPM          uint32
}

// ListUSB returns the USB print queues installed on this computer. Every queue is
// listed regardless of its name, which is what removes the "rename the printer to
// POS-80 first" step.
//
// The parse below walks Win32 memory, so a recover keeps a bad spooler response
// from taking the whole service (and every open POS tab) down with it.
func ListUSB() (list []Printer, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			list = nil
			err = &Error{
				Code:    "PRINTER_ENUMERATION_FAILED",
				Message: "Windows returned a printer list the agent could not read. Restart the LnkitsPrintAgent service and try again.",
			}
		}
	}()

	buf, returned, err := enumPrinters(printerEnumLocal | printerEnumConnections)
	if err != nil {
		return nil, err
	}
	if returned == 0 {
		return []Printer{}, nil
	}

	entrySize := unsafe.Sizeof(printerInfo2{})
	if uintptr(returned)*entrySize > uintptr(len(buf)) {
		return nil, &Error{
			Code:    "PRINTER_ENUMERATION_FAILED",
			Message: "Windows returned an inconsistent printer list. Restart the LnkitsPrintAgent service and try again.",
		}
	}

	list = make([]Printer, 0, returned)
	base := unsafe.Pointer(&buf[0])

	for index := 0; index < returned; index++ {
		info := (*printerInfo2)(unsafe.Add(base, uintptr(index)*entrySize))

		name := utf16PtrToString(info.pPrinterName)
		if name == "" || isVirtualQueue(name) {
			continue
		}
		port := utf16PtrToString(info.pPortName)
		driver := utf16PtrToString(info.pDriverName)
		instance, hasInstance := usbInstanceForPort(port)
		if !isUSBQueue(port, hasInstance) {
			continue
		}

		id := "port:" + strings.ToLower(port)
		if hasInstance {
			id = "usbprint:" + strings.ToLower(instance)
		}
		stale := info.Attributes&printerAttributeWorkOffline != 0 ||
			info.Status&printerStatusOffline != 0

		list = append(list, Printer{
			ID:        id,
			Kind:      "USB",
			Name:      name,
			Port:      port,
			Driver:    driver,
			Model:     modelFromInstance(instance),
			Connected: !stale,
			Stale:     stale,
			Status:    statusText(info.Status),
		})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Stale != list[j].Stale {
			return !list[i].Stale
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, nil
}

// enumPrinters calls EnumPrintersW at level 2 (PRINTER_INFO_2W), sizing the
// buffer from the value the spooler reports first.
func enumPrinters(flags uint32) ([]byte, int, error) {
	var needed, returned uint32
	procEnumPrintersW.Call(
		uintptr(flags), 2, 0, 0,
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)
	if needed == 0 {
		return nil, 0, nil
	}

	buf := make([]byte, needed)
	ok, _, callErr := procEnumPrintersW.Call(
		uintptr(flags), 2,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)
	if ok == 0 {
		return nil, 0, spoolerError(winErr(callErr), "the local printer list")
	}
	return buf, int(returned), nil
}
