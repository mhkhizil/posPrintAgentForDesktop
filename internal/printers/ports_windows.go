//go:build windows

package printers

import (
	"strings"
)

// usbInstanceForPort maps a spooler port (USB001) to the USB device instance
// Windows assigned to it. The USB print monitor keeps that mapping under its
// Ports key; the value name differs between Windows versions, so the first value
// that looks like a device instance is used.
func usbInstanceForPort(port string) (string, bool) {
	if strings.TrimSpace(port) == "" {
		return "", false
	}

	values, err := regEnumValues(`SYSTEM\CurrentControlSet\Control\Print\Monitors\USB Monitor\Ports\` + port)
	if err != nil {
		return "", false
	}
	for _, value := range values {
		if looksLikeDeviceInstance(value.Value) {
			return strings.TrimSpace(value.Value), true
		}
	}
	return "", false
}

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

func statusText(status uint32) string {
	switch {
	case status&printerStatusOffline != 0:
		return "offline"
	case status&printerStatusError != 0:
		return "error"
	case status&printerStatusPrinting != 0:
		return "printing"
	case status&printerStatusBusy != 0:
		return "busy"
	default:
		return "idle"
	}
}
