package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

	expectedEntries, err := util.CanonicalizeIPPoolEntries(ips)
	if err != nil {
		return err
	}
	expectedName := util.IPPoolAddressSetName(ippoolName)

	return wait.PollUntilContextTimeout(context.Background(), addressSetPollInterval, addressSetTimeout, true, func(_ context.Context) (bool, error) {
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

		actualEntries := util.NormalizeAddressSetEntries(strings.Join(as.Addresses, " "))
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

	return wait.PollUntilContextTimeout(context.Background(), addressSetPollInterval, addressSetTimeout, true, func(_ context.Context) (bool, error) {
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
		return "", errors.New("failed to resolve OVN NB endpoints")
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
