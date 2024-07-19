/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This file comes from sigs.k8s.io/controller-runtime/pkg/metrics/client_go_adapter.go

package metrics

import (
	"context"
	"net/url"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/metrics"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var requestLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "rest_client_request_latency_seconds",
		Help:    "Request latency in seconds. Broken down by verb and URL.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
	},
	[]string{"verb", "url"},
)

func InitClientGoMetrics() {
	registerClientMetrics()
}

// registerClientMetrics sets up the client latency metrics from client-go
func registerClientMetrics() {
	// register the metrics with our registry
	ctrlmetrics.Registry.MustRegister(requestLatency)

	// register the metrics with client-go
	metrics.RequestLatency = &latencyAdapter{metric: requestLatency}
}

type latencyAdapter struct {
	metric *prometheus.HistogramVec
}

func (l *latencyAdapter) Observe(_ context.Context, verb string, u url.URL, latency time.Duration) {
	l.metric.WithLabelValues(verb, path.Dir(u.Path)).Observe(latency.Seconds())
}
