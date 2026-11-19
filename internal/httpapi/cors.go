package httpapi

import (
	"net/http"
	"strings"
)

// applyCORS answers a browser preflight and reports whether the calling origin
// may use the agent. A request without an Origin header (curl, service checks)
// is allowed; the token still guards printing.
//
// Access-Control-Allow-Private-Network is what Chromium's Private Network
// Access check expects when a public HTTPS page calls a loopback address.
func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if !s.cfg.AllowsOrigin(origin) {
		return false
	}

	if s.cfg.AllowsEveryOrigin() {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+TokenHeader)
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	w.Header().Set("Access-Control-Max-Age", "600")
	return true
}
