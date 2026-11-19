// Package httpapi exposes the loopback HTTP contract the POS page calls.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/mhkhizil/vision-pos-print-agent/internal/config"
	"github.com/mhkhizil/vision-pos-print-agent/internal/printers"
)

const (
	// AgentName is the service name reported by /v1/health.
	AgentName = "pos-print-agent"
	// ContractVersion changes only when the JSON contract breaks.
	ContractVersion = 1
	// TokenHeader is how the POS page proves it read the token from /v1/health.
	TokenHeader = "X-Pos-Agent-Token"
)

// Server holds the configuration and the boot time used by /v1/health.
type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	version string
	started time.Time
}

// New builds a Server.
func New(cfg *config.Config, log *slog.Logger, version string) *Server {
	return &Server{cfg: cfg, log: log, version: version, started: time.Now()}
}

// Handler returns the router with CORS, token and recovery middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/printers", s.handlePrinters)
	mux.HandleFunc("POST /v1/print", s.handlePrint)
	mux.HandleFunc("POST /v1/probe", s.handleProbe)
	mux.HandleFunc("POST /v1/admin/cleanup", s.handleCleanup)
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("Cache-Control", "no-store")

		defer func() {
			if recovered := recover(); recovered != nil {
				s.log.Error("request panicked", "path", r.URL.Path, "panic", recovered)
				writeError(w, http.StatusInternalServerError, "INTERNAL", "The print agent hit an unexpected error.")
			}
		}()

		if !s.applyCORS(w, r) {
			s.log.Warn("blocked origin", "origin", r.Header.Get("Origin"), "path", r.URL.Path)
			writeError(w, http.StatusForbidden, "ORIGIN_NOT_ALLOWED",
				"This page is not allowed to use the print agent. Add its origin to allowOrigins in the agent configuration.")
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.URL.Path != "/v1/health" && !s.authorized(r) {
			s.log.Warn("rejected request", "path", r.URL.Path, "origin", r.Header.Get("Origin"))
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED",
				"The print agent rejected this request. Reload the POS page so it reads a fresh agent token.")
			return
		}

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"ms", time.Since(started).Milliseconds(),
			"origin", r.Header.Get("Origin"),
		)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	expected := s.cfg.Token
	if expected == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get(TokenHeader))
	if provided == "" {
		auth := r.Header.Get("Authorization")
		if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
			provided = strings.TrimSpace(auth[7:])
		}
	}
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (s *Server) capabilities() []string {
	capabilities := []string{string(printers.TransportNetwork), string(printers.TransportUSB)}
	if runtime.GOOS == "windows" {
		capabilities = append(capabilities, string(printers.TransportBluetooth))
	}
	if printers.IsElevated() {
		capabilities = append(capabilities, "PRINTER_CLEANUP")
	}
	return capabilities
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type errorResponse struct {
	Success  bool               `json:"success"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	Printers []printers.Printer `json:"printers,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Success: false, Code: code, Message: message})
}

// decodeJSON reads at most limit bytes into value.
func decodeJSON(r *http.Request, limit int64, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, limit))
	return decoder.Decode(value)
}

