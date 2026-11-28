package frameworkhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTimeoutTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Accept the connection and request, but never send response headers.
		<-r.Context().Done()
	}))
	defer server.Close()

	report := runCaseOnce(&http.Client{Timeout: 50 * time.Millisecond}, http.MethodGet, server.URL, http.StatusOK)
	if report == nil {
		t.Fatal("expected a timeout report")
	}
	for _, want := range []string{"connect start", "connect done", "local=", "remote=", "wrote request error=<nil>"} {
		if !strings.Contains(report.Trace, want) {
			t.Errorf("trace missing %q: %s", want, report.Trace)
		}
	}
	if strings.Contains(report.Trace, "got first response byte") {
		t.Errorf("unexpected response in timeout trace: %s", report.Trace)
	}
	if report.Elapsed < 50 || !strings.Contains(report.Attachments, "Client.Timeout") {
		t.Errorf("missing timeout evidence: %+v", report)
	}
}

func TestFailureHandlerBeforeNextRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	stop := make(chan struct{})
	var observed *Report
	reports, err := LoopWithFailureHandler("NodePort", server.URL, http.MethodGet, 0, 100, 1000, http.StatusOK, stop, func(r *Report) {
		observed = r
		close(stop)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || observed != reports[0] || requests != 1 {
		t.Fatalf("expected immediate callback and stop: reports=%v observed=%v requests=%d", reports, observed, requests)
	}
	if observed.Index != 0 || observed.Name != "NodePort" || !strings.Contains(observed.Trace, "got first response byte") {
		t.Fatalf("missing request identity or response trace: %+v", observed)
	}
}

func TestSuccessfulRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reports, err := LoopWithFailureHandler("NodePort", server.URL, http.MethodGet, 3, 0, 1000, http.StatusOK, nil, func(r *Report) {
		t.Errorf("unexpected failure callback: %+v", r)
	})
	if err != nil || len(reports) != 0 {
		t.Fatalf("expected successful requests: reports=%v error=%v", reports, err)
	}
}
