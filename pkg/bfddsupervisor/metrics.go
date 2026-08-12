package bfddsupervisor

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

type MetricsReporter struct {
	registry         *prometheus.Registry
	expectedSessions prometheus.Gauge
	upSessions       prometheus.Gauge
	recoveryAttempts *prometheus.CounterVec
	recoveryPhase    *prometheus.GaugeVec
	sessionZero      prometheus.Gauge
	childRestarts    prometheus.Counter
	childCircuitOpen prometheus.Gauge
	circuitOpen      *prometheus.GaugeVec

	mutex             sync.Mutex
	seenAttempts      map[string]int
	seenChildRestarts int
	initializedChild  bool
	zeroSince         time.Time
}

func NewMetricsReporter() *MetricsReporter {
	reporter := &MetricsReporter{
		registry: prometheus.NewRegistry(),
		expectedSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ovn_bfdd_expected_sessions",
			Help: "Number of BFD sessions expected by the supervisor.",
		}),
		upSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ovn_bfdd_up_sessions",
			Help: "Number of expected BFD sessions currently Up.",
		}),
		recoveryAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kube_ovn_bfdd_recovery_attempts_total",
			Help: "Number of bounded BFD recovery attempts.",
		}, []string{"action", "result"}),
		recoveryPhase: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kube_ovn_bfdd_recovery_phase",
			Help: "Current recovery phase for a BFD session.",
		}, []string{"local", "remote", "phase"}),
		sessionZero: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ovn_bfdd_session_zero_seconds",
			Help: "Seconds for which all expected BFD sessions have been non-Up.",
		}),
		childRestarts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kube_ovn_bfdd_child_restarts_total",
			Help: "Number of successful OpenBFDD child restarts.",
		}),
		childCircuitOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ovn_bfdd_child_circuit_open",
			Help: "Whether automatic OpenBFDD child recovery is circuit-open.",
		}),
		circuitOpen: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kube_ovn_bfdd_circuit_open",
			Help: "Whether automatic recovery is circuit-open for a BFD session.",
		}, []string{"local", "remote"}),
		seenAttempts: make(map[string]int),
	}
	reporter.registry.MustRegister(
		reporter.expectedSessions,
		reporter.upSessions,
		reporter.recoveryAttempts,
		reporter.recoveryPhase,
		reporter.sessionZero,
		reporter.childRestarts,
		reporter.childCircuitOpen,
		reporter.circuitOpen,
	)
	return reporter
}

func (r *MetricsReporter) Registry() *prometheus.Registry {
	return r.registry
}

func (r *MetricsReporter) Update(now time.Time, status SupervisorStatus) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.expectedSessions.Set(float64(len(status.Sessions)))
	up := 0
	r.recoveryPhase.Reset()
	r.circuitOpen.Reset()
	for _, session := range status.Sessions {
		if session.State == SessionUp {
			up++
		}
		r.recoveryPhase.WithLabelValues(session.Pair.Local, session.Pair.Remote, string(session.Phase)).Set(1)
		circuit := 0.0
		if session.Phase == RecoveryCircuitOpen {
			circuit = 1
		}
		r.circuitOpen.WithLabelValues(session.Pair.Local, session.Pair.Remote).Set(circuit)

		key := pairKey(session.Pair)
		previous, initialized := r.seenAttempts[key]
		if initialized && session.Attempts > previous && session.LastAction != RecoveryNone {
			result := "success"
			if session.LastResult != "success" {
				result = "error"
			}
			r.recoveryAttempts.WithLabelValues(string(session.LastAction), result).Add(float64(session.Attempts - previous))
		}
		r.seenAttempts[key] = session.Attempts
	}
	r.upSessions.Set(float64(up))
	if status.ChildCircuitOpen {
		r.childCircuitOpen.Set(1)
	} else {
		r.childCircuitOpen.Set(0)
	}
	if len(status.Sessions) != 0 && up == 0 {
		if r.zeroSince.IsZero() {
			r.zeroSince = now
		}
		r.sessionZero.Set(now.Sub(r.zeroSince).Seconds())
	} else {
		r.zeroSince = time.Time{}
		r.sessionZero.Set(0)
	}
	if r.initializedChild && status.ChildRestarts > r.seenChildRestarts {
		r.childRestarts.Add(float64(status.ChildRestarts - r.seenChildRestarts))
	}
	r.seenChildRestarts = status.ChildRestarts
	r.initializedChild = true
}

func (r *MetricsReporter) handler(status func() SupervisorStatus) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) {
		if !status().Live {
			http.Error(writer, "BFD supervisor is not live", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	return mux
}

func (r *MetricsReporter) Serve(ctx context.Context, address string, status func() SupervisorStatus) error {
	server := &http.Server{
		Addr:              address,
		Handler:           r.handler(status),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			klog.Errorf("failed to stop BFD supervisor metrics server: %v", err)
		}
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
