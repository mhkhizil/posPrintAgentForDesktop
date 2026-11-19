package httpapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhkhizil/vision-pos-print-agent/internal/printers"
)

const (
	minTimeoutMS = 500
	maxTimeoutMS = 60000
)

// jobFromRequest validates what the page sent and decodes the ESC/POS bytes.
func (s *Server) jobFromRequest(request printRequest) (printers.Job, error) {
	transport := printers.Transport(strings.ToUpper(strings.TrimSpace(request.Transport)))
	if !transport.Valid() {
		return printers.Job{}, &printers.Error{
			Code:    "UNSUPPORTED_TRANSPORT",
			Message: fmt.Sprintf("%q is not a printer connection this agent understands.", request.Transport),
		}
	}

	encoding := strings.ToLower(strings.TrimSpace(request.Encoding))
	if encoding != "" && encoding != "base64" {
		return printers.Job{}, &printers.Error{
			Code:    "UNSUPPORTED_ENCODING",
			Message: "Printer data must be base64.",
		}
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(request.DataBase64))
	if err != nil {
		return printers.Job{}, &printers.Error{
			Code:    "INVALID_DATA",
			Message: "The printer data is not valid base64.",
		}
	}
	if len(data) == 0 {
		return printers.Job{}, &printers.Error{
			Code:    "EMPTY_JOB",
			Message: "There is nothing to print.",
		}
	}
	if int64(len(data)) > s.cfg.MaxBodyBytes {
		return printers.Job{}, &printers.Error{
			Code:    "JOB_TOO_LARGE",
			Message: "This print job is larger than the agent allows (8 MB).",
		}
	}

	timeoutMS := request.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = s.cfg.DefaultTimeoutMS
	}
	if timeoutMS < minTimeoutMS {
		timeoutMS = minTimeoutMS
	}
	if timeoutMS > maxTimeoutMS {
		timeoutMS = maxTimeoutMS
	}

	job := printers.Job{
		JobID:     strings.TrimSpace(request.JobID),
		Transport: transport,
		PrinterID: strings.TrimSpace(request.PrinterID),
		DeviceID:  strings.TrimSpace(request.DeviceID),
		Host:      strings.TrimSpace(request.Host),
		Port:      request.Port,
		Data:      data,
		Timeout:   time.Duration(timeoutMS) * time.Millisecond,
	}

	switch transport {
	case printers.TransportNetwork:
		if job.Host == "" {
			return printers.Job{}, &printers.Error{
				Code:    "HOST_REQUIRED",
				Message: "Enter the printer's IP address.",
			}
		}
		if strings.ContainsAny(job.Host, " /\\") {
			return printers.Job{}, &printers.Error{
				Code:    "INVALID_HOST",
				Message: "Enter the printer's address as an IP address or host name, without a path.",
			}
		}
		if job.Port == 0 {
			job.Port = 9100
		}
	case printers.TransportUSB:
		if job.PrinterID == "" && job.DeviceID == "" {
			return printers.Job{}, &printers.Error{
				Code:    "PRINTER_REQUIRED",
				Message: "Choose a printer first.",
			}
		}
	case printers.TransportBluetooth:
		if job.DeviceID == "" {
			return printers.Job{}, &printers.Error{
				Code:    "PRINTER_REQUIRED",
				Message: "Choose the Bluetooth printer's COM port first.",
			}
		}
	}

	return job, nil
}

// writePrintError maps a printer failure to the HTTP status the page expects.
func (s *Server) writePrintError(w http.ResponseWriter, err error) {
	var printerErr *printers.Error
	if errors.As(err, &printerErr) {
		status := http.StatusInternalServerError
		switch printerErr.Code {
		case "UNSUPPORTED_TRANSPORT", "UNSUPPORTED_ENCODING", "INVALID_JSON", "INVALID_DATA",
			"EMPTY_JOB", "JOB_TOO_LARGE", "HOST_REQUIRED", "INVALID_HOST", "PRINTER_REQUIRED":
			status = http.StatusBadRequest
		case "PRINTER_NOT_FOUND", "PRINTER_OFFLINE", "PRINTER_BUSY", "PRINTER_ACCESS_DENIED":
			status = http.StatusConflict
		}
		writeJSON(w, status, errorResponse{
			Success:  false,
			Code:     printerErr.Code,
			Message:  printerErr.Message,
			Printers: printerErr.Printers,
		})
		return
	}

	switch {
	case errors.Is(err, printers.ErrNotElevated):
		writeError(w, http.StatusForbidden, "NOT_ELEVATED",
			"Cleaning up printers needs the print agent service to run as LocalSystem.")
		return
	case errors.Is(err, printers.ErrNotSupported):
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This action is not available on this platform.")
		return
	}

	writeError(w, http.StatusInternalServerError, "PRINTER_ERROR",
		"The print agent could not finish the print job.")
}

type probeRequest struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

type probeResponse struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Message   string `json:"message,omitempty"`
}

// handleProbe reports whether a network printer answers, without printing.
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	var request probeRequest
	if err := decodeJSON(r, s.cfg.MaxBodyBytes, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "The probe request could not be read.")
		return
	}
	host := strings.TrimSpace(request.Host)
	if host == "" || strings.ContainsAny(host, " /\\") {
		writeError(w, http.StatusBadRequest, "INVALID_HOST", "Enter the printer's IP address.")
		return
	}
	port := request.Port
	if port == 0 {
		port = 9100
	}

	started := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))), 5*time.Second)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, probeResponse{
			OK:        false,
			LatencyMs: latency,
			Message:   fmt.Sprintf("The printer at %s:%d did not answer.", host, port),
		})
		return
	}
	_ = conn.Close()
	writeJSON(w, http.StatusOK, probeResponse{OK: true, LatencyMs: latency})
}

// handleCleanup removes print queues whose printer is no longer attached.
func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	report, err := printers.Cleanup()
	if err != nil {
		s.writePrintError(w, err)
		return
	}
	s.log.Info("cleaned up stale printers",
		"removed", strings.Join(report.RemovedQueues, ", "),
		"failed", strings.Join(report.FailedQueues, ", "),
	)
	writeJSON(w, http.StatusOK, report)
}
