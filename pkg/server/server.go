package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/klog/v2"
)

func SecureServing(addr, svcName string, handler http.Handler) (<-chan struct{}, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("invalid listen address %q: %v", addr, err)
	}

	namespace := os.Getenv("POD_NAMESPACE")
	podName := os.Getenv("POD_NAME")
	podIPs := os.Getenv("POD_IPS")
	alternateDNS := []string{podName, svcName, fmt.Sprintf("%s.%s", svcName, namespace), fmt.Sprintf("%s.%s.svc", svcName, namespace)}
	alternateIPs := []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}
	for _, podIP := range strings.Split(podIPs, ",") {
		if ip := net.ParseIP(podIP); ip != nil {
			alternateIPs = append(alternateIPs, ip)
		}
	}

	opt := options.NewSecureServingOptions()
	opt.ServerCert.PairName = svcName
	opt.ServerCert.CertDirectory = ""
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil {
			err = fmt.Errorf("invalid listen address: %q", addr)
			klog.Error(err)
			return nil, err
		}
		opt.BindAddress = ip
		p, err := strconv.Atoi(port)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("invalid listen address %q: %v", addr, err)
		}
		opt.BindPort = p
	}

	if err = opt.MaybeDefaultWithSelfSignedCerts("localhost", alternateDNS, alternateIPs); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to genarate self signed certificates: %v", err)
	}

	var c *server.SecureServingInfo
	if err = opt.ApplyTo(&c); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to apply secure serving options to secure serving info: %v", err)
	}

	stopCh := make(chan struct{}, 1)
	_, listenerStoppedCh, err := c.Serve(handler, 0, stopCh)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to serve on %s: %v", addr, err)
	}

	return listenerStoppedCh, nil
}
