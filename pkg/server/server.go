package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"k8s.io/apiserver/pkg/endpoints/filters"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/scheme"
)

func SecureServing(addr, svcName string, handler http.Handler) (<-chan struct{}, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("invalid listen address %q: %w", addr, err)
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

	var clientConfig *rest.Config
	opt := options.NewSecureServingOptions().WithLoopback()
	authnOpt := options.NewDelegatingAuthenticationOptions()
	authzOpt := options.NewDelegatingAuthorizationOptions()
	opt.ServerCert.PairName = svcName
	opt.ServerCert.CertDirectory = ""
	authnOpt.RemoteKubeConfigFileOptional = true
	authzOpt.RemoteKubeConfigFileOptional = true

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
			return nil, fmt.Errorf("invalid listen address %q: %w", addr, err)
		}
		opt.BindPort = p
	}

	if err = opt.MaybeDefaultWithSelfSignedCerts("localhost", alternateDNS, alternateIPs); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to generate self signed certificates: %w", err)
	}

	var serving *server.SecureServingInfo
	var authn server.AuthenticationInfo
	var authz server.AuthorizationInfo
	if err = opt.ApplyTo(&serving, &clientConfig); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to apply secure serving options to secure serving info: %w", err)
	}
	if err = authnOpt.ApplyTo(&authn, serving, nil); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to apply authn options to authn info: %w", err)
	}
	if err = authzOpt.ApplyTo(&authz); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to apply authz options to authz info: %w", err)
	}

	handler = filters.WithAuthorization(handler, authz.Authorizer, scheme.Codecs)
	handler = filters.WithAuthentication(handler, authn.Authenticator, filters.Unauthorized(scheme.Codecs), nil, nil)

	requestInfoResolver := &request.RequestInfoFactory{}
	handler = filters.WithRequestInfo(handler, requestInfoResolver)
	handler = filters.WithCacheControl(handler)
	server.AuthorizeClientBearerToken(clientConfig, &authn, &authz)

	stopCh := make(chan struct{}, 1)
	_, listenerStoppedCh, err := serving.Serve(handler, 0, stopCh)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to serve on %s: %w", addr, err)
	}

	return listenerStoppedCh, nil
}
