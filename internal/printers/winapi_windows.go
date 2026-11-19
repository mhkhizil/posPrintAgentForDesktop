//go:build windows

package printers

import (
	"syscall"
	"unsafe"
)

// winspool and advapi32 are resolved lazily: the agent must start even on a
// machine where a printing component is disabled, and fail only when used.
var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	winspool = syscall.NewLazyDLL("winspool.drv")

	procOpenPrinterW     = winspool.NewProc("OpenPrinterW")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
	procStartDocPrinterW = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
	procDeletePrinter    = winspool.NewProc("DeletePrinter")
	procEnumPrintersW    = winspool.NewProc("EnumPrintersW")

	procRegEnumValueW = advapi32.NewProc("RegEnumValueW")
)

const (
	regSZ          = 1
	tokenElevation = 20
)

type regValue struct {
	Name  string
	Value string
}

// winErr normalises what LazyProc.Call reports: it always returns a non-nil
// error, with errno 0 when the call actually succeeded.
func winErr(err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return nil
	}
	return err
}

// utf16PtrToString reads a NUL-terminated UTF-16 string returned by Win32.
func utf16PtrToString(ptr *uint16) string {
	if ptr == nil {
		return ""
	}
	length := 0
	for cursor := unsafe.Pointer(ptr); *(*uint16)(cursor) != 0; cursor = unsafe.Add(cursor, 2) {
		length++
	}
	return syscall.UTF16ToString(unsafe.Slice(ptr, length))
}

// utf16BuffToString converts a fixed Win32 buffer, trimming the terminator.
func utf16BuffToString(buf []uint16, byteLen uint32) string {
	count := int(byteLen / 2)
	if count > len(buf) {
		count = len(buf)
	}
	for count > 0 && buf[count-1] == 0 {
		count--
	}
	if count == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:count])
}

// utf16InBuffer reads a NUL-terminated string that Win32 placed inside
// [base, end). A pointer outside that range yields "": a spooler response the
// agent misreads must not fault the whole service.
func utf16InBuffer(base, end unsafe.Pointer, ptr *uint16) string {
	if ptr == nil {
		return ""
	}
	start := uintptr(unsafe.Pointer(ptr))
	if start < uintptr(base) || start >= uintptr(end) {
		return ""
	}

	length := 0
	limit := uintptr(end)
	for cursor := unsafe.Pointer(ptr); uintptr(cursor) < limit; cursor = unsafe.Add(cursor, 2) {
		if *(*uint16)(cursor) == 0 {
			break
		}
		length++
	}
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice(ptr, length))
}

// regEnumValues reads every REG_SZ value under HKLM\<subkey>.
func regEnumValues(subkey string) ([]regValue, error) {
	path, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return nil, err
	}

	var key syscall.Handle
	if openErr := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ, &key); openErr != nil {
		return nil, openErr
	}
	defer syscall.RegCloseKey(key)

	values := make([]regValue, 0, 8)
	for index := uint32(0); ; index++ {
		var name [512]uint16
		nameLen := uint32(len(name))
		var data [2048]uint16
		dataLen := uint32(len(data) * 2)
		var valueType uint32

		result, _, _ := procRegEnumValueW.Call(
			uintptr(key),
			uintptr(index),
			uintptr(unsafe.Pointer(&name[0])),
			uintptr(unsafe.Pointer(&nameLen)),
			0,
			uintptr(unsafe.Pointer(&valueType)),
			uintptr(unsafe.Pointer(&data[0])),
			uintptr(unsafe.Pointer(&dataLen)),
		)
		if result != 0 {
			break
		}
		if valueType != regSZ {
			continue
		}
		values = append(values, regValue{
			Name:  syscall.UTF16ToString(name[:nameLen]),
			Value: utf16BuffToString(data[:], dataLen),
		})
	}
	return values, nil
}

// processElevated reports whether this process holds an elevated token.
func processElevated() bool {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return false
	}
	var token syscall.Token
	if openErr := syscall.OpenProcessToken(process, syscall.TOKEN_QUERY, &token); openErr != nil {
		return false
	}
	defer token.Close()

	var elevated uint32
	var returned uint32
	if infoErr := syscall.GetTokenInformation(
		token, tokenElevation,
		(*byte)(unsafe.Pointer(&elevated)), 4,
		&returned,
	); infoErr != nil {
		return false
	}
	return elevated != 0
}
