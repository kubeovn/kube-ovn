package frr

import (
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"k8s.io/klog/v2"
)

//go:embed frr-egw.conf.tmpl
var frrConfigTemplate string

//go:embed frr-daemons.conf
var frrDaemonsConfig string

type Config struct {
	LocalASN      string
	PeerASN       string
	RouterID      string
	Neighbours    []string
	VNI           string
	RouteTargets  []string
	EnableEVPN    bool
	Password      string
	HoldTime      string
	KeepaliveTime string
	ConnectTime   string
	EbgpMultiHop  bool
}

func CmdMain() {
	if err := renderFRRConfig(); err != nil {
		klog.Fatalf("failed to render FRR config: %v", err)
	}
	klog.Info("FRR configuration rendered successfully")
}

func renderFRRConfig() error {
	config := Config{
		LocalASN: os.Getenv("LOCAL_ASN"),
		PeerASN:  os.Getenv("PEER_ASN"),
		RouterID: os.Getenv("ROUTER_ID"),
		VNI:      os.Getenv("VNI"),
	}

	if neighbours := os.Getenv("NEIGHBOURS"); neighbours != "" {
		config.Neighbours = strings.Split(neighbours, ",")
		for i := range config.Neighbours {
			config.Neighbours[i] = strings.TrimSpace(config.Neighbours[i])
		}
	}

	if routeTargets := os.Getenv("ROUTE_TARGETS"); routeTargets != "" {
		config.RouteTargets = strings.Split(routeTargets, ",")
		for i := range config.RouteTargets {
			config.RouteTargets[i] = strings.TrimSpace(config.RouteTargets[i])
		}
	}

	config.EnableEVPN = config.VNI != ""

	config.Password = os.Getenv("BGP_PASSWORD")
	if holdTime := os.Getenv("BGP_HOLD_TIME"); holdTime != "" {
		if seconds, err := parseDurationToSeconds(holdTime); err == nil {
			config.HoldTime = seconds
		}
	}
	if keepaliveTime := os.Getenv("BGP_KEEPALIVE_TIME"); keepaliveTime != "" {
		if seconds, err := parseDurationToSeconds(keepaliveTime); err == nil {
			config.KeepaliveTime = seconds
		}
	}
	if connectTime := os.Getenv("BGP_CONNECT_TIME"); connectTime != "" {
		if seconds, err := parseDurationToSeconds(connectTime); err == nil {
			config.ConnectTime = seconds
		}
	}
	if ebgpMultiHop := os.Getenv("BGP_EBGP_MULTIHOP"); ebgpMultiHop != "" {
		config.EbgpMultiHop = ebgpMultiHop == "true"
	}

	if config.RouterID == "" {
		podIP := getFirstPodIP()
		if podIP == "" {
			return errors.New("ROUTER_ID not set and unable to determine pod IP")
		}
		config.RouterID = podIP
	}

	if err := validateConfig(&config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	if err := os.MkdirAll("/etc/frr", 0o755); err != nil {
		return fmt.Errorf("failed to create /etc/frr directory: %w", err)
	}

	if err := renderTemplateToFile(frrConfigTemplate, "/etc/frr/frr.conf", config); err != nil {
		return fmt.Errorf("failed to render frr.conf: %w", err)
	}

	if err := os.WriteFile("/etc/frr/daemons", []byte(frrDaemonsConfig), 0o600); err != nil {
		return fmt.Errorf("failed to write daemons config: %w", err)
	}

	return nil
}

func validateConfig(config *Config) error {
	if config.LocalASN == "" {
		return errors.New("LOCAL_ASN is required")
	}
	if _, err := strconv.ParseUint(config.LocalASN, 10, 32); err != nil {
		return fmt.Errorf("LOCAL_ASN must be a valid uint32: %w", err)
	}

	if config.PeerASN == "" {
		return errors.New("PEER_ASN is required")
	}
	if _, err := strconv.ParseUint(config.PeerASN, 10, 32); err != nil {
		return fmt.Errorf("PEER_ASN must be a valid uint32: %w", err)
	}

	if config.RouterID == "" {
		return errors.New("ROUTER_ID is required")
	}
	if net.ParseIP(config.RouterID) == nil {
		return errors.New("ROUTER_ID must be a valid IP address")
	}

	if len(config.Neighbours) == 0 {
		return errors.New("NEIGHBOURS is required")
	}
	for _, neighbour := range config.Neighbours {
		if net.ParseIP(neighbour) == nil {
			return fmt.Errorf("invalid neighbour IP: %s", neighbour)
		}
	}

	if config.VNI != "" {
		if _, err := strconv.ParseUint(config.VNI, 10, 32); err != nil {
			return fmt.Errorf("VNI must be a valid uint32: %w", err)
		}
	}

	return nil
}

func renderTemplateToFile(templateContent, outputPath string, data any) error {
	tmpl, err := template.New("frr").Parse(templateContent)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", outputPath, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

func getFirstPodIP() string {
	if podIP := os.Getenv("POD_IP"); podIP != "" {
		return podIP
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		klog.Errorf("failed to get interface addresses: %v", err)
		return ""
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}

	return ""
}

func parseDurationToSeconds(durationStr string) (string, error) {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return "", fmt.Errorf("invalid duration format %s: %w", durationStr, err)
	}

	seconds := int64(duration.Seconds())
	if seconds < 0 || seconds > 65535 {
		return "", fmt.Errorf("duration %s is out of valid range [0, 65535] seconds", durationStr)
	}

	return strconv.FormatInt(seconds, 10), nil
}
