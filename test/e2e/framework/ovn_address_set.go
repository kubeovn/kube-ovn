package framework

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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
	ovnNbModels   map[string]model.Model
	ovnClientErr  error
)

// WaitForAddressSetIPs waits for the OVN address set backing the given IPPool
// to contain exactly the provided entries (order independent).
func WaitForAddressSetIPs(ippoolName string, ips []string) error {
	client, models, err := getOVNNbClient(ovnnb.AddressSetTable)
	if err != nil {
		return err
	}

	// Use ExpandIPPoolAddressesForOVN to get the expected format (with simplified IPs)
	expectedEntries, err := util.ExpandIPPoolAddressesForOVN(ips)
	if err != nil {
		return err
	}

	asName := util.IPPoolAddressSetName(ippoolName)
	Logf("Waiting for address set %s of IPPool %s to have entries: %v", asName, ippoolName, expectedEntries)

	return wait.PollUntilContextTimeout(context.Background(), addressSetPollInterval, addressSetTimeout, true, func(_ context.Context) (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), client.Timeout)
		defer cancel()

		model := models[ovnnb.AddressSetTable]
		result := reflect.New(reflect.SliceOf(reflect.TypeOf(model).Elem())).Interface()
		if err := client.List(ctx, result); err != nil {
			return false, err
		}

		sets := make(map[string][]string, 1)
		for i := 0; i < reflect.ValueOf(result).Elem().Len(); i++ {
			externalIDs := reflect.ValueOf(result).Elem().Index(i).FieldByName("ExternalIDs")
			if !externalIDs.MapIndex(reflect.ValueOf(ippoolExternalIDKey)).Equal(reflect.ValueOf(ippoolName)) {
				continue
			}
			name := reflect.ValueOf(result).Elem().Index(i).FieldByName("Name").String()
			addrField := reflect.ValueOf(result).Elem().Index(i).FieldByName("Addresses")
			addresses := make([]string, 0, addrField.Len())
			for j := 0; j < addrField.Len(); j++ {
				addresses = append(addresses, addrField.Index(j).String())
			}
			sets[name] = addresses
		}

		setNames := slices.Collect(maps.Keys(sets))
		switch len(sets) {
		case 0:
			return false, nil
		case 1:
			if setNames[0] != asName {
				return false, fmt.Errorf("unexpected address set name %q for ippool %s, want %q", setNames[0], ippoolName, asName)
			}
		default:
			return false, fmt.Errorf("multiple address sets found for ippool %s: %s", ippoolName, strings.Join(setNames, ","))
		}

		addresses := sets[setNames[0]]
		actualEntries := util.NormalizeAddressSetEntries(strings.Join(addresses, " "))
		return actualEntries.Equal(set.New(expectedEntries...)), nil
	})
}

// WaitForAddressSetDeletion waits until OVN deletes the address set for the given IPPool.
func WaitForAddressSetDeletion(ippoolName string) error {
	client, models, err := getOVNNbClient(ovnnb.AddressSetTable)
	if err != nil {
		return err
	}

	Logf("Waiting for address set of IPPool %s to be deleted", ippoolName)

	return wait.PollUntilContextTimeout(context.Background(), addressSetPollInterval, addressSetTimeout, true, func(_ context.Context) (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), client.Timeout)
		defer cancel()

		model := models[ovnnb.AddressSetTable]
		result := reflect.New(reflect.SliceOf(reflect.TypeOf(model).Elem())).Interface()
		if err := client.List(ctx, result); err != nil {
			return false, err
		}

		var sets []string
		for i := 0; i < reflect.ValueOf(result).Elem().Len(); i++ {
			externalIDs := reflect.ValueOf(result).Elem().Index(i).FieldByName("ExternalIDs")
			if !externalIDs.MapIndex(reflect.ValueOf(ippoolExternalIDKey)).Equal(reflect.ValueOf(ippoolName)) {
				continue
			}
			name := reflect.ValueOf(result).Elem().Index(i).FieldByName("Name").String()
			sets = append(sets, name)
		}

		if len(sets) > 1 {
			return false, fmt.Errorf("multiple address sets found for ippool %s: %s", ippoolName, strings.Join(sets, ","))
		}

		if len(sets) != 0 {
			Logf("Found address sets for IPPool %s: %s", ippoolName, strings.Join(sets, ","))
		}
		return len(sets) == 0, nil
	})
}

func getOVNNbClient(tables ...string) (*ovs.OVNNbClient, map[string]model.Model, error) {
	ovnClientOnce.Do(func() {
		conn, err := resolveOVNNbConnection()
		if err != nil {
			ovnClientErr = err
			return
		}
		ovnNbClient, ovnNbModels, ovnClientErr = ovs.NewDynamicOvnNbClient(
			conn,
			ovnNbTimeoutSeconds,
			ovsdbConnTimeout,
			ovsdbInactivityTimeout,
			tables...,
		)
	})
	return ovnNbClient, ovnNbModels, ovnClientErr
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
