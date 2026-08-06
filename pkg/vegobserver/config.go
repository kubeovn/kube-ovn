package vegobserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	DefaultListenAddress  = ":10666"
	DefaultHealthCheckURL = "http://127.0.0.1:10666/healthz"
	DefaultCacheCapacity  = 65536
	DefaultEventBuffer    = 4096
	DefaultLogQueue       = 4096
)

// Config is the complete, hot-reloadable observer configuration written by the controller.
type Config struct {
	Namespace       string                              `json:"namespace"`
	Name            string                              `json:"name"`
	ExternalNetwork string                              `json:"externalNetwork"`
	Observability   apiv1.VpcEgressGatewayObservability `json:"observability"`
}

type observerIdentity struct {
	namespace string
	name      string
	pod       string
	node      string
}

func (i observerIdentity) labels() []string {
	return []string{i.namespace, i.name, i.pod, i.node}
}

func (i observerIdentity) matches(config Config) bool {
	return config.Namespace == i.namespace && config.Name == i.name
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read observer config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode observer config: %w", err)
	}
	if config.Namespace == "" || config.Name == "" || config.ExternalNetwork == "" {
		return Config{}, errors.New("observer config requires namespace, name and externalNetwork")
	}
	return config, nil
}

func conntrackEnabled(config Config) bool {
	return config.Observability.Conntrack.Metrics.Enabled || config.Observability.Conntrack.Log.Enabled
}
