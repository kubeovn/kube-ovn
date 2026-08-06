package vegobserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Options defines process-level observer inputs. Runtime feature configuration is loaded from ConfigPath.
type Options struct {
	ConfigPath        string
	NetworkStatusPath string
	ListenAddress     string
	Pod               string
	Node              string
	Stdout            io.Writer
	Stderr            io.Writer
}

// Run starts configuration reload, collectors, flow logging and the private HTTP registry.
func Run(ctx context.Context, options Options) error {
	if options.ConfigPath == "" || options.NetworkStatusPath == "" {
		return errors.New("config and network-status paths are required")
	}
	if options.ListenAddress == "" {
		options.ListenAddress = DefaultListenAddress
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	config, err := loadConfig(options.ConfigPath)
	if err != nil {
		return err
	}
	filters, err := compileFilters(config.Observability.Conntrack.Log.Filters)
	if err != nil {
		return err
	}
	identity := observerIdentity{namespace: config.Namespace, name: config.Name, pod: options.Pod, node: options.Node}
	labels := identity.labels()
	metrics := newObserverMetrics()
	settings := &atomic.Pointer[runtimeSettings]{}
	settings.Store(&runtimeSettings{config: config, filters: filters, limiter: newLimiter(config.Observability.Conntrack.Log.RateLimit)})
	metrics.configReloads.WithLabelValues(append(labels, "success")...).Inc()

	logQueue := make(chan flowRecord, DefaultLogQueue)
	go writeFlowLogs(ctx, options.Stdout, identity, metrics, logQueue)
	go reloadConfig(ctx, options.ConfigPath, identity, metrics, settings, options.Stderr)
	conntrackCollector := newConntrackCollector(settings.Load, identity, metrics, logQueue)
	go conntrackCollector.run(ctx, options.Stderr)
	interfaceCollector := newInterfaceCollector(options.NetworkStatusPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if _, err := writer.Write([]byte("ok\n")); err != nil {
			metrics.errors.WithLabelValues(append(labels, "http", "healthz_write")...).Inc()
		}
	})
	metricsHandler := promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{DisableCompression: true})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, request *http.Request) {
		current := settings.Load()
		if current != nil && current.config.Observability.InterfaceMetrics.Enabled {
			if err := interfaceCollector.update(current.config, identity, metrics); err != nil {
				metrics.collectorUp.WithLabelValues(append(labels, "interface")...).Set(0)
				metrics.errors.WithLabelValues(append(labels, "interface", "collect")...).Inc()
				writeDiagnostic(metrics, identity, options.Stderr, "interface", "interface collector: %v\n", err)
			} else {
				metrics.collectorUp.WithLabelValues(append(labels, "interface")...).Set(1)
			}
		} else {
			metrics.collectorUp.WithLabelValues(append(labels, "interface")...).Set(0)
			for _, collector := range metrics.interfaceCounters {
				collector.Reset()
			}
		}
		metricsHandler.ServeHTTP(writer, request)
	})

	server := &http.Server{Addr: options.ListenAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("observer HTTP server: %w", err)
	}
}

// CheckHealth verifies the observer HTTP server through its loopback endpoint.
func CheckHealth(ctx context.Context, address string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request observer health endpoint: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("observer health endpoint returned %s", response.Status)
	}
	return nil
}

func reloadConfig(ctx context.Context, path string, identity observerIdentity, metrics *observerMetrics, settings *atomic.Pointer[runtimeSettings], diagnostics io.Writer) {
	labels := identity.labels()
	currentHash, err := configHash(settings.Load().config)
	if err != nil {
		metrics.configReloads.WithLabelValues(append(labels, "error")...).Inc()
		writeDiagnostic(metrics, identity, diagnostics, "config", "hash observer config: %v\n", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			config, err := loadConfig(path)
			if err != nil {
				metrics.configReloads.WithLabelValues(append(labels, "error")...).Inc()
				writeDiagnostic(metrics, identity, diagnostics, "config", "reload observer config: %v\n", err)
				continue
			}
			nextHash, err := configHash(config)
			if err != nil {
				metrics.configReloads.WithLabelValues(append(labels, "error")...).Inc()
				writeDiagnostic(metrics, identity, diagnostics, "config", "hash reloaded observer config: %v\n", err)
				continue
			}
			if nextHash == currentHash {
				continue
			}
			if !identity.matches(config) {
				metrics.configReloads.WithLabelValues(append(labels, "error")...).Inc()
				writeDiagnostic(metrics, identity, diagnostics, "config", "reload observer config: gateway identity is immutable\n")
				continue
			}
			filters, err := compileFilters(config.Observability.Conntrack.Log.Filters)
			if err != nil {
				metrics.configReloads.WithLabelValues(append(labels, "error")...).Inc()
				writeDiagnostic(metrics, identity, diagnostics, "config", "reload observer filters: %v\n", err)
				continue
			}
			settings.Store(&runtimeSettings{config: config, filters: filters, limiter: newLimiter(config.Observability.Conntrack.Log.RateLimit)})
			currentHash = nextHash
			metrics.configReloads.WithLabelValues(append(labels, "success")...).Inc()
		}
	}
}

func configHash(config Config) ([sha256.Size]byte, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode observer config: %w", err)
	}
	return sha256.Sum256(data), nil
}

func writeDiagnostic(metrics *observerMetrics, identity observerIdentity, writer io.Writer, collector, format string, arguments ...any) {
	if _, err := fmt.Fprintf(writer, format, arguments...); err != nil {
		metrics.errors.WithLabelValues(append(identity.labels(), collector, "diagnostic_write")...).Inc()
	}
}
