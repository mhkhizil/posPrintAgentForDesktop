package httpapi

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/mhkhizil/vision-pos-print-agent/internal/printers"
)

type healthResponse struct {
	OK              bool     `json:"ok"`
	AgentVersion    string   `json:"agentVersion"`
	ContractVersion int      `json:"contractVersion"`
	Platform        string   `json:"platform"`
	Elevated        bool     `json:"elevated"`
	Capabilities    []string `json:"capabilities"`
	AllowOrigins    []string `json:"allowOrigins"`
	UptimeMs        int64    `json:"uptimeMs"`
	Token           string   `json:"token"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		OK:              true,
		AgentVersion:    s.version,
		ContractVersion: ContractVersion,
		Platform:        runtime.GOOS,
		Elevated:        printers.IsElevated(),
		Capabilities:    s.capabilities(),
		AllowOrigins:    s.cfg.AllowOrigins,
		UptimeMs:        time.Since(s.started).Milliseconds(),
		Token:           s.cfg.Token,
	})
}

type printersResponse struct {
	Printers []printers.Printer `json:"printers"`
}

func (s *Server) handlePrinters(w http.ResponseWriter, r *http.Request) {
	transport := printers.Transport(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("transport"))))

	var (
		list []printers.Printer
		err  error
	)
	switch transport {
	case printers.TransportUSB:
		list, err = printers.ListUSB()
	case printers.TransportBluetooth:
		list, err = printers.ListSerial()
	case printers.TransportNetwork, "":
		list = []printers.Printer{}
	default:
		writeError(w, http.StatusBadRequest, "UNSUPPORTED_TRANSPORT",
			"Use transport=USB, transport=BLUETOOTH or leave it empty.")
		return
	}
	if err != nil {
		s.writePrintError(w, err)
		return
	}
	if list == nil {
		list = []printers.Printer{}
	}
	writeJSON(w, http.StatusOK, printersResponse{Printers: list})
}

type printRequest struct {
	JobID      string `json:"jobId"`
	Transport  string `json:"transport"`
	PrinterID  string `json:"printerId"`
	DeviceID   string `json:"deviceId"`
	Host       string `json:"host"`
	Port       uint16 `json:"port"`
	DataBase64 string `json:"dataBase64"`
	Encoding   string `json:"encoding"`
	TimeoutMS  int    `json:"timeoutMs"`
}

func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)

	var request printRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON",
			"The print job could not be read. Check that the agent and the POS page use the same contract version.")
		return
	}

	job, err := s.jobFromRequest(request)
	if err != nil {
		s.writePrintError(w, err)
		return
	}

	report, err := printers.Print(r.Context(), job)
	if err != nil {
		s.log.Warn("print failed",
			"job", job.JobID,
			"transport", job.Transport,
			"printer", job.PrinterID,
			"device", job.DeviceID,
			"error", err.Error(),
		)
		s.writePrintError(w, err)
		return
	}

	printerName, printerPort := "", ""
	if report.Printer != nil {
		printerName = report.Printer.Name
		printerPort = report.Printer.Port
	}
	s.log.Info("printed",
		"job", job.JobID,
		"transport", job.Transport,
		"printer", printerName,
		"port", printerPort,
		"via", report.Via,
		"bytes", report.BytesWritten,
		"rebound", report.Rebound,
		"ms", report.DurationMs,
	)
	writeJSON(w, http.StatusOK, report)
}
