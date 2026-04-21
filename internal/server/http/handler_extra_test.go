package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"seckill-agent/internal/testkit"
)

func TestHandler_ChatDebug_InvalidJSON(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &testkit.SequenceLLM{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader([]byte(`{invalid json`)))
	req.Header.Set("Content-Type", "application/json")

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_AgentRun_EmptyTask(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &testkit.SequenceLLM{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/run", bytes.NewReader([]byte(`{"task":""}`)))
	req.Header.Set("Content-Type", "application/json")

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_ConcurrentHealthz(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &testkit.SequenceLLM{}, nil)
	handler := h.Routes()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	}
}
