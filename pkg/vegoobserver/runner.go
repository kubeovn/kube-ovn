package vegoobserver

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
	identity := []string{config.Namespace, config.Name, options.Pod, options.Node}
	metrics := newObserverMetrics()
	settings := &atomic.Pointer[runtimeSettings]{}
	settings.Store(&runtimeSettings{config: config, filters: filters, limiter: newLimiter(config.Observability.Conntrack.Log.RateLimit)})
	metrics.configReloads.WithLabelValues(append(identity, "success")...).Inc()

	logQueue := make(chan flowRecord, DefaultLogQueue)
	go writeFlowLogs(ctx, options.Stdout, identity, metrics, logQueue)
	go reloadConfig(ctx, options.ConfigPath, identity, metrics, settings, options.Stderr)
	conntrackCollector := newConntrackCollector(settings.Load, identity, metrics, logQueue)
	go conntrackCollector.run(ctx, options.Stderr)
	interfaceCollector := newInterfaceCollector(options.NetworkStatusPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	metricsHandler := promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{DisableCompression: true})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, request *http.Request) {
		current := settings.Load()
		if current != nil && current.config.Observability.InterfaceMetrics.Enabled {
			if err := interfaceCollector.update(current.config, identity, metrics); err != nil {
				metrics.collectorUp.WithLabelValues(append(identity, "interface")...).Set(0)
				metrics.errors.WithLabelValues(append(identity, "interface", "collect")...).Inc()
				_, _ = fmt.Fprintf(options.Stderr, "interface collector: %v\n", err)
			} else {
				metrics.collectorUp.WithLabelValues(append(identity, "interface")...).Set(1)
			}
		} else {
			metrics.collectorUp.WithLabelValues(append(identity, "interface")...).Set(0)
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

func reloadConfig(ctx context.Context, path string, identity []string, metrics *observerMetrics, settings *atomic.Pointer[runtimeSettings], diagnostics io.Writer) {
	currentHash := configHash(settings.Load().config)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			config, err := loadConfig(path)
			if err != nil {
				metrics.configReloads.WithLabelValues(append(identity, "error")...).Inc()
				_, _ = fmt.Fprintf(diagnostics, "reload observer config: %v\n", err)
				continue
			}
			nextHash := configHash(config)
			if nextHash == currentHash {
				continue
			}
			if config.Namespace != identity[0] || config.Name != identity[1] {
				metrics.configReloads.WithLabelValues(append(identity, "error")...).Inc()
				_, _ = fmt.Fprintln(diagnostics, "reload observer config: gateway identity is immutable")
				continue
			}
			filters, err := compileFilters(config.Observability.Conntrack.Log.Filters)
			if err != nil {
				metrics.configReloads.WithLabelValues(append(identity, "error")...).Inc()
				_, _ = fmt.Fprintf(diagnostics, "reload observer filters: %v\n", err)
				continue
			}
			settings.Store(&runtimeSettings{config: config, filters: filters, limiter: newLimiter(config.Observability.Conntrack.Log.RateLimit)})
			currentHash = nextHash
			metrics.configReloads.WithLabelValues(append(identity, "success")...).Inc()
		}
	}
}

func configHash(config Config) [sha256.Size]byte {
	data, _ := json.Marshal(config)
	return sha256.Sum256(data)
}
