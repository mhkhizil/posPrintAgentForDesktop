package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhkhizil/vision-pos-print-agent/internal/config"
)

func testServer() (*Server, http.Handler) {
	cfg := &config.Config{
		Listen:           "127.0.0.1:8735",
		AllowOrigins:     []string{"https://pos.example.com"},
		Token:            "test-token",
		LogLevel:         "error",
		MaxBodyBytes:     8 << 20,
		DefaultTimeoutMS: 8000,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(cfg, logger, "test")
	return server, server.Handler()
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, recorder.Body.String())
	}
	return payload
}

func TestHealthReturnsContractAndToken(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	request.Header.Set("Origin", "https://pos.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	if origin := recorder.Header().Get("Access-Control-Allow-Origin"); origin != "https://pos.example.com" {
		t.Fatalf("origin %q was not echoed", origin)
	}
	payload := decodeBody(t, recorder)
	if payload["contractVersion"].(float64) != ContractVersion {
		t.Fatalf("contract version %v", payload["contractVersion"])
	}
	if payload["token"] != "test-token" {
		t.Fatalf("token %v", payload["token"])
	}
}

func TestPrintRequiresTheToken(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/print", strings.NewReader(`{"transport":"NETWORK","host":"10.0.0.5","dataBase64":"G0A="}`))
	request.Header.Set("Origin", "https://pos.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", recorder.Code)
	}
}

func TestPrintRejectsAnUnknownTransport(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/print",
		strings.NewReader(`{"transport":"SERIAL","deviceId":"COM1","dataBase64":"G0A="}`))
	request.Header.Set("Origin", "https://pos.example.com")
	request.Header.Set(TokenHeader, "test-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", recorder.Code)
	}
	if payload := decodeBody(t, recorder); payload["code"] != "UNSUPPORTED_TRANSPORT" {
		t.Fatalf("code %v", payload["code"])
	}
}

func TestPrintRejectsEmptyData(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/print",
		strings.NewReader(`{"transport":"NETWORK","host":"10.0.0.5","dataBase64":""}`))
	request.Header.Set("Origin", "https://pos.example.com")
	request.Header.Set(TokenHeader, "test-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", recorder.Code)
	}
	if payload := decodeBody(t, recorder); payload["code"] != "EMPTY_JOB" {
		t.Fatalf("code %v", payload["code"])
	}
}

func TestPreflightFromAnotherOriginIsBlocked(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodOptions, "/v1/print", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	request.Header.Set("Access-Control-Request-Method", "POST")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", recorder.Code)
	}
}

func TestPreflightAllowsThePrivateNetworkHeader(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodOptions, "/v1/print", nil)
	request.Header.Set("Origin", "https://pos.example.com")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Private-Network", "true")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("PNA header %q", got)
	}
}

func TestCleanupRequiresTheToken(t *testing.T) {
	_, handler := testServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/admin/cleanup", nil)
	request.Header.Set("Origin", "https://pos.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", recorder.Code)
	}
}
