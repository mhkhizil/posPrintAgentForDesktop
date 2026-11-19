//go:build !windows

package printers

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ListUSB lists CUPS queues. macOS and Linux reach a USB printer through CUPS,
// so the queue is the printer's address here too.
func ListUSB() ([]Printer, error) {
	out, err := exec.Command("lpstat", "-a").Output()
	if err != nil {
		return []Printer{}, nil
	}
	list := []Printer{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		list = append(list, Printer{
			ID:        "cups:" + strings.ToLower(name),
			Kind:      "USB",
			Name:      name,
			Driver:    "CUPS",
			Connected: true,
			Status:    "idle",
		})
	}
	return list, nil
}

// printSpooler sends the job through CUPS with the raw option, so the queue's
// filter does not reinterpret the ESC/POS bytes.
func printSpooler(name string, data []byte) (int, error) {
	cmd := exec.Command("lp", "-d", name, "-o", "raw")
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, &Error{
			Code:    "PRINTER_ERROR",
			Message: fmt.Sprintf("CUPS could not print to %q: %s", name, strings.TrimSpace(stderr.String())),
		}
	}
	return len(data), nil
}

// printSerial is Windows-only; the agent's Bluetooth transport targets Windows
// POS terminals where SPP is exposed as a COM port.
func printSerial(port string, data []byte, timeout time.Duration) (int, error) {
	if port == "" {
		return 0, &Error{Code: "PRINTER_REQUIRED", Message: "Choose the Bluetooth printer first."}
	}
	return 0, ErrNotSupported
}

// ListSerial has no portable Bluetooth SPP enumeration.
func ListSerial() ([]Printer, error) { return []Printer{}, nil }

// IsElevated reports whether the agent runs as root.
func IsElevated() bool { return os.Geteuid() == 0 }

// Cleanup is a Windows-only maintenance action.
func Cleanup() (CleanupReport, error) { return CleanupReport{}, ErrNotSupported }
