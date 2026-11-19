//go:build windows

package printers

import (
	"strings"
)

// looksLikeDeviceInstance accepts the PnP instance id the USB print monitor keeps
// for a port, e.g. USBPRINT\EPSONTM-T88V\4&2b9f1c3a&0&USB001.
func looksLikeDeviceInstance(value string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.Contains(trimmed, `\`) {
		return false
	}
	upper := strings.ToUpper(trimmed)
	return strings.HasPrefix(upper, `USBPRINT\`) || strings.HasPrefix(upper, `USB\`)
}

// isUSBQueue keeps the queues an ESC/POS receipt printer actually uses. DOT4 is
// what some USB drivers install instead of USB001.
func isUSBQueue(port string, hasInstance bool) bool {
	if hasInstance {
		return true
	}
	upper := strings.ToUpper(strings.TrimSpace(port))
	return strings.HasPrefix(upper, "USB") || strings.HasPrefix(upper, "DOT4")
}

// isVirtualQueue drops the Windows pseudo-printers that are never a receipt
// printer, whatever port they happen to use.
func isVirtualQueue(name string) bool {
	virtual := []string{
		"microsoft print to pdf", "microsoft xps document writer", "onenote",
		"fax", "send to onenote", "microsoft shared fax driver",
	}
	lower := strings.ToLower(name)
	for _, candidate := range virtual {
		if strings.Contains(lower, candidate) {
			return true
		}
	}
	return false
}

// modelFromInstance reads the model token out of e.g.
// USBPRINT\EPSONTM-T88V\4&2b9f1c3a&0&USB001.
func modelFromInstance(instance string) string {
	parts := strings.Split(instance, `\`)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// statusText maps a PrinterStatus value from Get-Printer.
func statusText(status int) string {
	switch status {
	case 4:
		return "printing"
	case 7, 11:
		return "offline"
	case 9:
		return "error"
	case 10:
		return "busy"
	default:
		return "idle"
	}
}
