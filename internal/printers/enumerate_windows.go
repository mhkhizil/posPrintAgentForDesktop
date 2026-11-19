//go:build windows

package printers

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
)

// Queues are listed through the PrintManagement module rather than
// EnumPrintersW at level 2. PRINTER_INFO_2W hands back pointers into a caller
// buffer, so reading it from Go means unsafe code in the request path — and a
// bad read there takes the whole service (and every open POS tab) down with it.
// Get-Printer returns the same facts as plain text.
//
// The same script resolves each port to the USB device instance Windows assigned
// to it, which is the stable identity that survives a queue being renamed.
const listPrintersScript = `$ErrorActionPreference = 'SilentlyContinue'
$ports = @{}
foreach ($key in Get-ChildItem 'HKLM:\SYSTEM\CurrentControlSet\Control\Print\Monitors\USB Monitor\Ports') {
  $found = $null
  foreach ($prop in (Get-ItemProperty $key.PSPath).PSObject.Properties) {
    if ($prop.Value -is [string] -and $prop.Value -match '^USB(PRINT)?\\') { $found = $prop.Value; break }
  }
  if ($found) { $ports[$key.PSChildName] = $found }
}
foreach ($printer in Get-Printer) {
  [pscustomobject]@{
    name     = [string]$printer.Name
    port     = [string]$printer.PortName
    driver   = [string]$printer.DriverName
    status   = [int]$printer.PrinterStatus
    instance = [string]$ports[[string]$printer.PortName]
  } | ConvertTo-Json -Compress
}`

// printerRow is one line of the script's output.
type printerRow struct {
	Name     string `json:"name"`
	Port     string `json:"port"`
	Driver   string `json:"driver"`
	Status   int    `json:"status"`
	Instance string `json:"instance"`
}

// ListUSB returns the USB print queues installed on this computer. Every queue is
// listed regardless of its name, which is what removes the "rename the printer to
// POS-80 first" step.
func ListUSB() ([]Printer, error) {
	output, err := exec.Command(
		"powershell.exe",
		"-NoProfile", "-NonInteractive", "-Command", listPrintersScript,
	).Output()
	if err != nil {
		return nil, &Error{
			Code:    "PRINTER_ENUMERATION_FAILED",
			Message: "Windows would not list its printers. Check that the Print Spooler service is running, then try again.",
		}
	}

	list := []Printer{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var row printerRow
		if jsonErr := json.Unmarshal([]byte(line), &row); jsonErr != nil {
			continue
		}
		if row.Name == "" || isVirtualQueue(row.Name) {
			continue
		}

		instance := strings.TrimSpace(row.Instance)
		hasInstance := looksLikeDeviceInstance(instance)
		if !isUSBQueue(row.Port, hasInstance) {
			continue
		}

		id := "port:" + strings.ToLower(row.Port)
		if hasInstance {
			id = "usbprint:" + strings.ToLower(instance)
		}

		stale := row.Status == 7 || row.Status == 11 // Offline, Not Available
		list = append(list, Printer{
			ID:        id,
			Kind:      "USB",
			Name:      row.Name,
			Port:      row.Port,
			Driver:    row.Driver,
			Model:     modelFromInstance(instance),
			Connected: !stale,
			Stale:     stale,
			Status:    statusText(row.Status),
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
