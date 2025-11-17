package framework

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

const (
	addressSetPollInterval = 2 * time.Second
	addressSetTimeout      = 2 * time.Minute
	ippoolExternalIDKey    = "ippool"
	ovnNbTimeoutSeconds    = 60
	ovsdbConnTimeout       = 3
	ovsdbInactivityTimeout = 10
	ovnClientMaxRetry      = 5
)

var (
	ovnClientOnce sync.Once
	ovnNbClient   *ovs.OVNNbClient
	ovnClientErr  error
)

// WaitForAddressSetIPs waits for the OVN address set backing the given IPPool
// to contain exactly the provided entries (order independent).
func WaitForAddressSetIPs(ippoolName string, ips []string) error {
	client, err := getOVNNbClient()
	if err != nil {
		return err
	}

	expectedEntries, err := canonicalizeIPPoolEntries(ips)
	if err != nil {
		return err
	}
	expectedName := ippoolAddressSetName(ippoolName)

	return wait.PollUntilContextTimeout(context.Background(), addressSetPollInterval, addressSetTimeout, true, func(ctx context.Context) (bool, error) {
		sets, err := client.ListAddressSets(map[string]string{ippoolExternalIDKey: ippoolName})
		if err != nil {
			return false, err
		}
		if len(sets) == 0 {
			return false, nil
		}
		if len(sets) > 1 {
			return false, fmt.Errorf("multiple address sets found for ippool %s", ippoolName)
		}

		as := sets[0]
		if as.Name != expectedName {
			return false, fmt.Errorf("unexpected address set name %q for ippool %s, want %q", as.Name, ippoolName, expectedName)
		}

		actualEntries := normalizeAddressSetEntries(strings.Join(as.Addresses, " "))
		if len(actualEntries) != len(expectedEntries) {
			return false, nil
		}

		for entry := range expectedEntries {
			if !actualEntries[entry] {
				return false, nil
			}
		}

		return true, nil
	})
}

// WaitForAddressSetDeletion waits until OVN deletes the address set for the given IPPool.
func WaitForAddressSetDeletion(ippoolName string) error {
	client, err := getOVNNbClient()
	if err != nil {
		return err
	}

	return wait.PollUntilContextTimeout(context.Background(), addressSetPollInterval, addressSetTimeout, true, func(ctx context.Context) (bool, error) {
		sets, err := client.ListAddressSets(map[string]string{ippoolExternalIDKey: ippoolName})
		if err != nil {
			return false, err
		}
		if len(sets) > 1 {
			return false, fmt.Errorf("multiple address sets found for ippool %s", ippoolName)
		}
		return len(sets) == 0, nil
	})
}

func getOVNNbClient() (*ovs.OVNNbClient, error) {
	ovnClientOnce.Do(func() {
		conn, err := resolveOVNNbConnection()
		if err != nil {
			ovnClientErr = err
			return
		}
		ovnNbClient, ovnClientErr = ovs.NewOvnNbClient(conn, ovnNbTimeoutSeconds, ovsdbConnTimeout, ovsdbInactivityTimeout, ovnClientMaxRetry)
	})
	return ovnNbClient, ovnClientErr
}

func resolveOVNNbConnection() (string, error) {
	config, err := k8sframework.LoadConfig()
	if err != nil {
		return "", err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	var (
		enableSSL bool
		dbIPs     string
	)

	deploy, err := client.AppsV1().Deployments(KubeOvnNamespace).Get(ctx, "kube-ovn-controller", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name != "kube-ovn-controller" {
			continue
		}
		for _, env := range container.Env {
			switch env.Name {
			case "ENABLE_SSL":
				enableSSL = strings.EqualFold(strings.TrimSpace(env.Value), "true")
			case "OVN_DB_IPS":
				dbIPs = strings.TrimSpace(env.Value)
			}
		}
		break
	}

	protocol := "tcp"
	if enableSSL {
		protocol = "ssl"
	}

	var targets []string
	port := int32(6641)

	if dbIPs != "" {
		for _, host := range splitAndTrim(dbIPs) {
			targets = append(targets, fmt.Sprintf("%s:[%s]:%d", protocol, host, port))
		}
	} else {
		svc, err := client.CoreV1().Services(KubeOvnNamespace).Get(ctx, "ovn-nb", metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if len(svc.Spec.Ports) > 0 && svc.Spec.Ports[0].Port != 0 {
			port = svc.Spec.Ports[0].Port
		}

		if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != corev1.ClusterIPNone {
			targets = append(targets, fmt.Sprintf("%s:[%s]:%d", protocol, svc.Spec.ClusterIP, port))
		} else {
			eps, err := client.CoreV1().Endpoints(KubeOvnNamespace).Get(ctx, svc.Name, metav1.GetOptions{})
			if err != nil {
				return "", err
			}
			for _, subset := range eps.Subsets {
				for _, address := range subset.Addresses {
					targets = append(targets, fmt.Sprintf("%s:[%s]:%d", protocol, address.IP, port))
				}
			}
		}
	}

	if len(targets) == 0 {
		return "", fmt.Errorf("failed to resolve OVN NB endpoints")
	}

	return strings.Join(targets, ","), nil
}

func splitAndTrim(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func canonicalizeIPPoolEntries(entries []string) (map[string]bool, error) {
	set := make(map[string]bool)
	seen := make(map[string]struct{})

	for _, raw := range entries {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		var tokens []string
		switch {
		case strings.Contains(value, ".."):
			parts := strings.Split(value, "..")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid IP range %q", value)
			}
			start, err := normalizeIPValue(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range start %q: %w", parts[0], err)
			}
			end, err := normalizeIPValue(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range end %q: %w", parts[1], err)
			}
			if (start.To4() != nil) != (end.To4() != nil) {
				return nil, fmt.Errorf("range %q mixes IPv4 and IPv6", value)
			}
			if compareIP(start, end) > 0 {
				return nil, fmt.Errorf("range %q start is greater than end", value)
			}
			rangeCIDRs, err := ipRangeToCIDRs(start, end)
			if err != nil {
				return nil, fmt.Errorf("failed to convert range %q: %w", value, err)
			}
			tokens = rangeCIDRs
		case strings.Contains(value, "/"):
			_, network, err := net.ParseCIDR(value)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", value, err)
			}
			tokens = []string{network.String()}
		default:
			ip, err := normalizeIPValue(value)
			if err != nil {
				return nil, fmt.Errorf("invalid IP address %q: %w", value, err)
			}
			tokens = []string{ip.String()}
		}

		for _, token := range tokens {
			if _, exists := seen[token]; exists {
				continue
			}
			seen[token] = struct{}{}
			set[token] = true
		}
	}

	return set, nil
}

func normalizeAddressSetEntries(raw string) map[string]bool {
	clean := strings.ReplaceAll(raw, "\"", "")
	tokens := strings.Fields(strings.TrimSpace(clean))
	set := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		set[token] = true
	}
	return set
}

func ippoolAddressSetName(ippoolName string) string {
	return strings.ReplaceAll(ippoolName, "-", ".")
}

func normalizeIPValue(value string) (net.IP, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address %q", value)
	}
	if v4 := ip.To4(); v4 != nil {
		return v4, nil
	}
	return ip.To16(), nil
}

func ipRangeToCIDRs(start, end net.IP) ([]string, error) {
	length := net.IPv4len
	totalBits := 32
	if start.To4() == nil {
		length = net.IPv6len
		totalBits = 128
	}

	startInt := ipToBigInt(start)
	endInt := ipToBigInt(end)
	if startInt.Cmp(endInt) > 0 {
		return nil, fmt.Errorf("range %s..%s start is greater than end", start, end)
	}

	result := make([]string, 0)
	tmp := new(big.Int)
	for startInt.Cmp(endInt) <= 0 {
		zeros := countTrailingZeros(startInt, totalBits)
		diff := tmp.Sub(endInt, startInt)
		diff.Add(diff, big.NewInt(1))

		var maxDiff uint
		if diff.Sign() > 0 {
			maxDiff = uint(diff.BitLen() - 1)
		}

		size := zeros
		if maxDiff < size {
			size = maxDiff
		}

		prefix := totalBits - int(size)
		networkInt := new(big.Int).Set(startInt)
		networkIP := bigIntToIP(networkInt, length)
		network := &net.IPNet{IP: networkIP, Mask: net.CIDRMask(prefix, totalBits)}
		result = append(result, network.String())

		increment := new(big.Int).Lsh(big.NewInt(1), size)
		startInt.Add(startInt, increment)
	}

	return result, nil
}

func ipToBigInt(ip net.IP) *big.Int {
	return new(big.Int).SetBytes(ip)
}

func bigIntToIP(value *big.Int, length int) net.IP {
	bytes := value.Bytes()
	if len(bytes) < length {
		padded := make([]byte, length)
		copy(padded[length-len(bytes):], bytes)
		bytes = padded
	} else if len(bytes) > length {
		bytes = bytes[len(bytes)-length:]
	}

	ip := make(net.IP, length)
	copy(ip, bytes)
	if length == net.IPv4len {
		return net.IP(ip).To4()
	}
	return net.IP(ip)
}

func countTrailingZeros(value *big.Int, totalBits int) uint {
	if value.Sign() == 0 {
		return uint(totalBits)
	}

	var zeros uint
	for zeros < uint(totalBits) && value.Bit(int(zeros)) == 0 {
		zeros++
	}
	return zeros
}

func compareIP(a, b net.IP) int {
	return bytes.Compare(a, b)
}
