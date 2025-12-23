package metrics

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog/v2"
	netutil "k8s.io/utils/net"
	"k8s.io/utils/ptr"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	altDNS []string
	altIPs = []net.IP{{127, 0, 0, 1}, net.IPv6loopback}
)

func init() {
	hostname, err := os.Hostname()
	if err != nil {
		panic(fmt.Sprintf("failed to get hostname: %v", err))
	}
	altDNS = []string{hostname}
	for podIP := range strings.SplitSeq(os.Getenv(util.EnvPodIPs), ",") {
		if podIP = strings.TrimSpace(podIP); podIP == "" {
			continue
		}
		if ip := net.ParseIP(podIP); ip != nil {
			altIPs = append(altIPs, ip)
		} else {
			panic(fmt.Sprintf("failed to parse environment variable %s=%q", util.EnvPodIPs, os.Getenv(util.EnvPodIPs)))
		}
	}
}

const caCommonName = "self-signed-ca"

func tlsGetConfigForClient(config *tls.Config) (func(*tls.ClientHelloInfo) (*tls.Config, error), error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	caCert, err := certutil.NewSelfSignedCACert(certutil.Config{CommonName: caCommonName}, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create self-signed CA cert: %w", err)
	}

	caBundle := pem.EncodeToMemory(&pem.Block{Type: certutil.CertificateBlockType, Bytes: caCert.Raw})
	caProvider, err := dynamiccertificates.NewStaticCAContent(caCommonName, caBundle)
	if err != nil {
		return nil, fmt.Errorf("failed to create static CA content provider: %w", err)
	}

	certKeyProvider, err := NewDynamicInMemoryCertKeyPairContent("localhost", caCert, caKey, altIPs, altDNS)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic in-memory cert/key pair content provider: %w", err)
	}

	controller := dynamiccertificates.NewDynamicServingCertificateController(config, caProvider, certKeyProvider, nil, nil)
	caProvider.AddListener(controller)
	certKeyProvider.AddListener(controller)

	// generate a context from stopCh. This is to avoid modifying files which are relying on apiserver
	// TODO: See if we can pass ctx to the current method
	stopCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-stopCh:
			cancel() // stopCh closed, so cancel our context
		case <-ctx.Done():
		}
	}()

	if controller, ok := certKeyProvider.(dynamiccertificates.ControllerRunner); ok {
		if err = controller.RunOnce(ctx); err != nil {
			err = fmt.Errorf("failed to run initial serving certificate population: %w", err)
			klog.Error(err)
			return nil, err
		}
		go controller.Run(ctx, 1)
	}

	if err = controller.RunOnce(); err != nil {
		return nil, fmt.Errorf("failed to run initial serving certificate population: %w", err)
	}
	go controller.Run(1, stopCh)

	return controller.GetConfigForClient, nil
}

func GenerateSelfSignedCertKey(host string, caCert *x509.Certificate, caKey *rsa.PrivateKey, alternateIPs []net.IP, alternateDNS []string) ([]byte, []byte, *time.Time, error) {
	now := time.Now().Truncate(0)
	validFrom := now.Add(-time.Hour) // valid an hour earlier to avoid flakes due to clock skew
	maxAge := time.Hour * 24 * 365   // one year self-signed certs

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}
	// returns a uniform random value in [0, max-1), then add 1 to serial to make it a uniform random value in [1, max).
	serial, err := rand.Int(rand.Reader, new(big.Int).SetInt64(math.MaxInt64-1))
	if err != nil {
		return nil, nil, nil, err
	}
	serial = new(big.Int).Add(serial, big.NewInt(1))
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: host,
		},
		NotBefore: validFrom,
		NotAfter:  validFrom.Add(maxAge),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := netutil.ParseIPSloppy(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}

	template.IPAddresses = append(template.IPAddresses, alternateIPs...)
	template.DNSNames = append(template.DNSNames, alternateDNS...)

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, caCert, &priv.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// Generate cert
	certBuffer := bytes.Buffer{}
	if err := pem.Encode(&certBuffer, &pem.Block{Type: certutil.CertificateBlockType, Bytes: derBytes}); err != nil {
		return nil, nil, nil, err
	}

	// Generate key
	keyBuffer := bytes.Buffer{}
	if err := pem.Encode(&keyBuffer, &pem.Block{Type: keyutil.RSAPrivateKeyBlockType, Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return nil, nil, nil, err
	}

	return certBuffer.Bytes(), keyBuffer.Bytes(), ptr.To(template.NotAfter), nil
}

type DynamicInMemoryCertKeyPairContent struct {
	host         string
	caCert       *x509.Certificate
	caKey        *rsa.PrivateKey
	alternateIPs []net.IP
	alternateDNS []string

	// certKeyPair is a certKeyPair that contains the last read, non-zero length content of the key and cert
	certKeyPair atomic.Value

	expireTime time.Time

	listeners []dynamiccertificates.Listener
}

var (
	_ dynamiccertificates.CertKeyContentProvider = &DynamicInMemoryCertKeyPairContent{}
	_ dynamiccertificates.ControllerRunner       = &DynamicInMemoryCertKeyPairContent{}
)

func NewDynamicInMemoryCertKeyPairContent(host string, caCert *x509.Certificate, caKey *rsa.PrivateKey, alternateIPs []net.IP, alternateDNS []string) (dynamiccertificates.CertKeyContentProvider, error) {
	ret := &DynamicInMemoryCertKeyPairContent{
		host:         host,
		caCert:       caCert,
		caKey:        caKey,
		alternateIPs: alternateIPs,
		alternateDNS: alternateDNS,
	}
	if err := ret.generateCertKeyPair(); err != nil {
		return nil, err
	}

	return ret, nil
}

// AddListener adds a listener to be notified when the serving cert content changes.
func (c *DynamicInMemoryCertKeyPairContent) AddListener(listener dynamiccertificates.Listener) {
	c.listeners = append(c.listeners, listener)
}

func (c *DynamicInMemoryCertKeyPairContent) generateCertKeyPair() error {
	if !c.expireTime.IsZero() && time.Now().Truncate(0).Before(c.expireTime.Add(-30*24*time.Hour)) {
		return nil
	}

	klog.Infof("Generating new cert/key pair for %s", c.host)
	cert, key, expire, err := GenerateSelfSignedCertKey(c.host, c.caCert, c.caKey, c.alternateIPs, c.alternateDNS)
	if err != nil {
		return fmt.Errorf("failed to generate self-signed cert/key pair: %w", err)
	}

	c.certKeyPair.Store(&certKeyPair{
		cert: cert,
		key:  key,
	})
	c.expireTime = *expire
	klog.Infof("Loaded newly generated cert/key pair with expiration time %s", c.expireTime.Local().Format(time.RFC3339))

	for _, listener := range c.listeners {
		listener.Enqueue()
	}

	return nil
}

// RunOnce runs a single sync loop
func (c *DynamicInMemoryCertKeyPairContent) RunOnce(_ context.Context) error {
	return c.generateCertKeyPair()
}

// Run starts the controller and blocks until context is killed.
func (c *DynamicInMemoryCertKeyPairContent) Run(ctx context.Context, _ int) {
	defer runtime.HandleCrash()

	klog.Info("Starting dynamic in-memory cert/key pair controller")
	defer klog.Info("Shutting down dynamic in-memory cert/key pair controller")

	go wait.Until(func() {
		if err := c.generateCertKeyPair(); err != nil {
			klog.ErrorS(err, "Failed to generate cert/key pair, will retry later")
		}
	}, time.Hour, ctx.Done())

	<-ctx.Done()
}

// Name is just an identifier
func (c *DynamicInMemoryCertKeyPairContent) Name() string {
	return ""
}

// CurrentCertKeyContent provides cert and key byte content
func (c *DynamicInMemoryCertKeyPairContent) CurrentCertKeyContent() ([]byte, []byte) {
	certKeyPair := c.certKeyPair.Load().(*certKeyPair)
	return certKeyPair.cert, certKeyPair.key
}

// certKeyPair holds the content for the cert and key
type certKeyPair struct {
	cert []byte
	key  []byte
}

func (c *certKeyPair) Equal(rhs *certKeyPair) bool {
	if c == nil || rhs == nil {
		return c == rhs
	}

	return bytes.Equal(c.key, rhs.key) && bytes.Equal(c.cert, rhs.cert)
}
