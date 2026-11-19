// Package printers turns an already-encoded ESC/POS byte stream into bytes on a
// printer: a raw TCP socket, a Windows print queue, or a serial (Bluetooth SPP)
// port. It never builds receipt content — the POS frontend does that.
package printers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Transport is the connection kind stored on a POS printer binding.
type Transport string

const (
	TransportNetwork   Transport = "NETWORK"
	TransportUSB       Transport = "USB"
	TransportBluetooth Transport = "BLUETOOTH"
)

var (
	// ErrNotElevated is returned by operations that need administrator rights.
	ErrNotElevated = errors.New("the print agent is not running elevated")
	// ErrNotSupported is returned on platforms where a transport is unavailable.
	ErrNotSupported = errors.New("this operation is not supported on this platform")
)

// Printer is one addressable printer as the POS settings screen shows it.
type Printer struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Port      string `json:"port,omitempty"`
	Driver    string `json:"driver,omitempty"`
	Model     string `json:"model,omitempty"`
	VendorID  string `json:"vendorId,omitempty"`
	ProductID string `json:"productId,omitempty"`
	Serial    string `json:"serial,omitempty"`
	Connected bool   `json:"connected"`
	Stale     bool   `json:"stale"`
	Status    string `json:"status,omitempty"`
}

// Job is a validated print request.
type Job struct {
	JobID     string
	Transport Transport
	PrinterID string
	DeviceID  string
	Host      string
	Port      uint16
	Data      []byte
	Timeout   time.Duration
}

// Report is what a successful print returns.
type Report struct {
	Success      bool     `json:"success"`
	BytesWritten int      `json:"bytesWritten"`
	Via          string   `json:"via"`
	DurationMs   int64    `json:"durationMs"`
	Rebound      bool     `json:"rebound"`
	Printer      *Printer `json:"printer,omitempty"`
}

// CleanupReport is the result of removing stale print queues.
type CleanupReport struct {
	RemovedQueues []string `json:"removedQueues"`
	FailedQueues  []string `json:"failedQueues,omitempty"`
}

// Error is a failure the HTTP layer can map to a status code and a POS message.
type Error struct {
	Code     string    `json:"code"`
	Message  string    `json:"message"`
	Printers []Printer `json:"printers,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Valid reports whether t is a transport this agent understands.
func (t Transport) Valid() bool {
	switch t {
	case TransportNetwork, TransportUSB, TransportBluetooth:
		return true
	}
	return false
}

// Print sends job.Data to the printer the job points at.
func Print(ctx context.Context, job Job) (Report, error) {
	started := time.Now()
	var (
		written int
		via     string
		target  *Printer
		rebound bool
		err     error
	)

	switch job.Transport {
	case TransportNetwork:
		written, err = printNetwork(ctx, job.Host, job.Port, job.Data, job.Timeout)
		via = "tcp"
	case TransportUSB:
		written, via, target, rebound, err = printUSB(job)
	case TransportBluetooth:
		written, err = printSerial(job.DeviceID, job.Data, job.Timeout)
		via = "serial"
	default:
		return Report{}, &Error{
			Code:    "UNSUPPORTED_TRANSPORT",
			Message: fmt.Sprintf("%q is not a printer connection this agent understands", job.Transport),
		}
	}
	if err != nil {
		return Report{}, err
	}

	return Report{
		Success:      true,
		BytesWritten: written,
		Via:          via,
		DurationMs:   time.Since(started).Milliseconds(),
		Rebound:      rebound,
		Printer:      target,
	}, nil
}

// printUSB resolves the saved printer to the queue that exists right now, then
// writes a RAW job to it. A queue Windows re-created after a port change is
// found by identity, so the operator never re-picks the printer by hand.
func printUSB(job Job) (int, string, *Printer, bool, error) {
	list, err := ListUSB()
	if err != nil {
		return 0, "spooler", nil, false, err
	}

	target, rebound := resolveUSB(job, list)
	if target == nil {
		return 0, "spooler", nil, false, &Error{
			Code:     "PRINTER_NOT_FOUND",
			Message:  notFoundMessage(job, list),
			Printers: list,
		}
	}

	written, printErr := printSpooler(target.Name, job.Data)
	if printErr == nil {
		return written, "spooler", target, rebound, nil
	}

	// The printer may have come back under another queue name. Re-enumerate once
	// and follow the same device before reporting the failure.
	if fresh, freshErr := ListUSB(); freshErr == nil {
		if retry, _ := resolveUSB(job, fresh); retry != nil && retry.Name != target.Name {
			if retried, retryErr := printSpooler(retry.Name, job.Data); retryErr == nil {
				return retried, "spooler", retry, true, nil
			}
		}
	}

	return 0, "spooler", target, false, printErr
}

// resolveUSB finds the queue a job belongs to. Order matters: the stable device
// identity wins, so a renamed queue still prints.
func resolveUSB(job Job, list []Printer) (*Printer, bool) {
	if len(list) == 0 {
		return nil, false
	}

	if job.PrinterID != "" {
		for i := range list {
			if strings.EqualFold(list[i].ID, job.PrinterID) {
				return &list[i], false
			}
		}
	}

	if job.DeviceID != "" {
		for i := range list {
			if strings.EqualFold(list[i].Name, job.DeviceID) {
				rebound := job.PrinterID != "" && !strings.EqualFold(list[i].ID, job.PrinterID)
				return &list[i], rebound
			}
		}
		for i := range list {
			if strings.EqualFold(list[i].Port, job.DeviceID) {
				return &list[i], true
			}
		}
	}

	// One printer is plugged in and the saved one is gone: that printer is what
	// the operator means. With two or more we refuse rather than print a receipt
	// on the wrong counter.
	connected := make([]*Printer, 0, len(list))
	for i := range list {
		if !list[i].Stale {
			connected = append(connected, &list[i])
		}
	}
	if len(connected) == 1 {
		return connected[0], true
	}

	return nil, false
}

func notFoundMessage(job Job, list []Printer) string {
	name := job.PrinterID
	if name == "" {
		name = job.DeviceID
	}
	if name == "" {
		name = "the saved printer"
	}
	if len(list) == 0 {
		return "No USB printer is installed on this computer. Install its Windows driver, plug it in, then try again."
	}
	names := make([]string, 0, len(list))
	for _, printer := range list {
		if printer.Name != "" {
			names = append(names, printer.Name)
		}
	}
	return fmt.Sprintf(
		"%q is not one of the installed USB print queues (%s). Open Settings > Printer and pick the connected printer.",
		name, strings.Join(names, ", "),
	)
}

