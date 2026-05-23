package util

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivezHandlerDefault(t *testing.T) {
	RegisterLivezProbe(nil)
	t.Cleanup(func() { RegisterLivezProbe(nil) })

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	LivezHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", string(body))
	}
}

func TestLivezHandlerProbeHealthy(t *testing.T) {
	RegisterLivezProbe(func() error { return nil })
	t.Cleanup(func() { RegisterLivezProbe(nil) })

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	LivezHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestLivezHandlerProbeFailing(t *testing.T) {
	probeErr := errors.New("socket unreachable")
	RegisterLivezProbe(func() error { return probeErr })
	t.Cleanup(func() { RegisterLivezProbe(nil) })

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	LivezHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
}

func TestRegisterLivezProbeNilClears(t *testing.T) {
	RegisterLivezProbe(func() error { return errors.New("boom") })
	RegisterLivezProbe(nil)
	t.Cleanup(func() { RegisterLivezProbe(nil) })

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	LivezHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 after clearing probe, got %d", rec.Code)
	}
}
