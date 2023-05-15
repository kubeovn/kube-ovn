package util

import (
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestValidateSubnet(t *testing.T) {

	os.Setenv("KUBERNETES_SERVICE_HOST", "10.20.0.1")
	tests := []struct {
		name    string
		asubnet kubeovnv1.Subnet
		err     string
	}{
		{
			name: "correct",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "utest",
					GenerateName:               "",
					Namespace:                  "",
					SelfLink:                   "",
					UID:                        "",
					ResourceVersion:            "",
					Generation:                 0,
					CreationTimestamp:          metav1.Time{},
					DeletionTimestamp:          nil,
					DeletionGracePeriodSeconds: nil,
					Labels:                     nil,
					Annotations:                nil,
					OwnerReferences:            nil,
					Finalizers:                 nil,
					ManagedFields:              nil,
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:                true,
					Vpc:                    "ovn-cluster",
					Protocol:               "IPv4",
					Namespaces:             nil,
					CIDRBlock:              "10.16.0.0/16",
					Gateway:                "10.16.0.1",
					ExcludeIps:             []string{"10.16.0.1"},
					Provider:               "ovn",
					GatewayType:            "distributed",
					GatewayNode:            "",
					NatOutgoing:            false,
					ExternalEgressGateway:  "",
					PolicyRoutingPriority:  0,
					PolicyRoutingTableID:   0,
					Private:                false,
					AllowSubnets:           nil,
					Vlan:                   "",
					Vips:                   nil,
					LogicalGateway:         false,
					DisableGatewayCheck:    false,
					DisableInterConnection: false,
					EnableDHCP:             false,
					DHCPv4Options:          "",
					DHCPv6Options:          "",
					EnableIPv6RA:           false,
					IPv6RAConfigs:          "",
					Acls:                   nil,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "",
		},
		{
			name: "gatewayErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-gatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.17.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "gateway 10.17.0.1 is not in cidr 10.16.0.0/16",
		},
		{
			name: "CIDRUnicastErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-unicasterr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "127.0.0.1/8",
					Gateway:     "127.0.0.1",
					ExcludeIps:  []string{"127.0.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "127.0.0.1/8 conflict with v4 loopback cidr 127.0.0.1/8",
		},
		{
			name: "CIDRNotIPErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-cidryerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "127.0.1/8",
					Gateway:     "127.0.0.1",
					ExcludeIps:  []string{"127.0.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "gateway 127.0.0.1 is not in cidr 127.0.1/8",
		},
		{
			name: "CIDRNotIPErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-cidrerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "127.0.1/8",
					Gateway:     "127.0.0.1",
					ExcludeIps:  []string{"127.0.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "gateway 127.0.0.1 is not in cidr 127.0.1/8",
		},
		{
			name: "ExcludeIPFormatErr1",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-excludeiperr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1..10.16.0.10..10.16.0.12"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "in excludeIps is not a valid ip range",
		},
		{
			name: "ExcludeIPFormatErr2",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-excludeiperr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1.."},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "exclude_ips is not a valid address",
		},
		{
			name: "ExcludeIPNotIPErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-excludeiperr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.1..10.16.10"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "in exclude_ips is not a valid address",
		},
		{
			name: "ExcludeIPRangeErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-excludecidrerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.2..10.16.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "10.16.0.2..10.16.0.1 in excludeIps is not a valid ip range",
		},
		{
			name: "AllowCIDRErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-allowcidrerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:      true,
					Vpc:          "ovn-cluster",
					Protocol:     "IPv4",
					Namespaces:   nil,
					CIDRBlock:    "10.16.0.0/16",
					Gateway:      "10.16.0.1",
					ExcludeIps:   []string{"10.16.0.1..10.16.0.10"},
					Provider:     "ovn",
					GatewayType:  "distributed",
					Private:      true,
					AllowSubnets: []string{"10.18.0/16"},
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "10.18.0/16 in allowSubnets is not a valid address",
		},
		{
			name: "gatewaytypeErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-gatewaytypeerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1..10.16.0.10"},
					Provider:    "ovn",
					GatewayType: "damn",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "damn is not a valid gateway type",
		},
		{
			name: "apiserverSVCErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-apisvcerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.20.0.0/16",
					Gateway:     "10.20.0.1",
					ExcludeIps:  []string{"10.20.0.1..10.20.0.10"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet utest-apisvcerr cidr 10.20.0.0/16 conflicts with k8s apiserver svc ip 10.20.0.1",
		},
		{
			name: "ExgressGWErr1",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-exgatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:               true,
					Vpc:                   "ovn-cluster",
					Protocol:              "IPv4",
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.1..10.16.0.10"},
					Provider:              "ovn",
					GatewayType:           "distributed",
					ExternalEgressGateway: "192.178.2.1",
					NatOutgoing:           true,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "conflict configuration: natOutgoing and externalEgressGateway",
		},
		{
			name: "ExgressGWErr2",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-exgatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:               true,
					Vpc:                   "ovn-cluster",
					Protocol:              "IPv4",
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.2..10.16.0.10"},
					Provider:              "ovn",
					GatewayType:           "distributed",
					ExternalEgressGateway: "192.178.2.1,192.178.2.2,192.178.2.3",
					NatOutgoing:           false,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "invalid external egress gateway configuration",
		},
		{
			name: "ExgressGWErr3",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-exgatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:               true,
					Vpc:                   "ovn-cluster",
					Protocol:              "IPv4",
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.2..10.16.0.10"},
					Provider:              "ovn",
					GatewayType:           "distributed",
					ExternalEgressGateway: "192.178.2.1,192.178..2",
					NatOutgoing:           false,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "IP 192.178..2 in externalEgressGateway is not a valid address",
		},
		{
			name: "ExgressGWErr4",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-exgatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:               true,
					Vpc:                   "ovn-cluster",
					Protocol:              "IPv4",
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.1"},
					Provider:              "ovn",
					GatewayType:           "distributed",
					ExternalEgressGateway: "192.178.2.1,fd00:10:16::1",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "invalid external egress gateway configuration: address family is conflict with CIDR",
		},
		{
			name: "ExgressGWErr5",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-exgatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1..10.16.0.10"},
					Provider:    "ovn",
					GatewayType: "distributed",
					Vips:        []string{"10.17.2.1"},
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "vip 10.17.2.1 conflicts with subnet utest-exgatewayerr cidr 10.16.0.0/16",
		},
		{
			name: "CIDRformErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "gateway 10.16.0.1 is not in cidr 10.16.0/16",
		},
		{
			name: "ExcludeIPErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "ip 10.16.1 in exclude_ips is not a valid address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := ValidateSubnet(tt.asubnet)
			if !ErrorContains(ret, tt.err) {
				t.Errorf("got %v, want a %v", ret, tt.err)
			}
		})
	}
}

func TestValidatePodNetwork(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		err         string
	}{
		{
			name: "podIP",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.16.0.15",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
				"ovn.kubernetes.io/cidr":         "10.16.0.0/16",
			},
			err: "",
		},
		{
			name: "podIPDual",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.244.0.0/16,fd00:10:244:0:2::/80",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
			},
			err: "",
		},
		{
			name: "podIPErr1",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.244.000.0/16,fd00:10:244:0:2::/80",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
			},
			err: "10.244.000.0/16 is not a valid ovn.kubernetes.io/ip_address",
		},
		{
			name: "podIPNotCIDRErr",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.244.0.0/16,fd00:10:244:0:2::::",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
			},
			err: "fd00:10:244:0:2:::: is not a valid ovn.kubernetes.io/ip_address",
		},
		{
			name: "podIPCIDRErr",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.244.0.0/16,fd00:10:244:0:2::/80",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
				"ovn.kubernetes.io/cidr":         "10.16.0/16",
			},
			err: "invalid cidr 10.16.0/16",
		},
		{
			name: "podIPErr4",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.244.0.0/16,fd00:10:244:0:2::/80",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
				"ovn.kubernetes.io/cidr":         "10.16.0.0/16",
			},
			err: "10.244.0.0/16 not in cidr 10.16.0.0/16",
		},
		{
			name: "podMacErr",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.16.0.15",
				"ovn.kubernetes.io/mac_address":  "00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
				"ovn.kubernetes.io/cidr":         "10.16.0.0/16",
			},
			err: "00:00:54:17:2A is not a valid ovn.kubernetes.io/mac_address",
		},
		{
			name: "podIPPollErr",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.16.0.15",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.1111.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "1",
				"ovn.kubernetes.io/cidr":         "10.16.0.0/16",
			},
			err: "10.16.1111.15,10.16.0.16,10.16.0.17 not in cidr 10.16.0.0/16",
		},
		{
			name: "ingRaErr",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.16.0.15",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "a3",
				"ovn.kubernetes.io/egress_rate":  "1",
				"ovn.kubernetes.io/cidr":         "10.16.0.0/16",
			},
			err: "a3 is not a valid ovn.kubernetes.io/ingress_rate",
		},
		{
			name: "EgRatErr",
			annotations: map[string]string{
				"ovn.kubernetes.io/ip_address":   "10.16.0.15",
				"ovn.kubernetes.io/mac_address":  "00:00:00:54:17:2A",
				"ovn.kubernetes.io/ip_pool":      "10.16.0.15,10.16.0.16,10.16.0.17",
				"ovn.kubernetes.io/ingress_rate": "3",
				"ovn.kubernetes.io/egress_rate":  "a1",
				"ovn.kubernetes.io/cidr":         "10.16.0.0/16",
			},
			err: "a1 is not a valid ovn.kubernetes.io/egress_rate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := ValidatePodNetwork(tt.annotations)
			if !ErrorContains(ret, tt.err) {
				t.Errorf("got %v, want a error %v", ret, tt.err)
			}
		})
	}
}

func TestValidatePodCidr(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		ip   string
		err  string
	}{
		{
			name: "corretV4",
			cidr: "10.16.0.0/16",
			ip:   "10.16.0.3",
			err:  "",
		},
		{
			name: "corretDual",
			cidr: "10.244.0.0/16,fd00:10:244:0:2::/80",
			ip:   "10.244.0.6,fd00:10:244:0:2:2",
			err:  "",
		},
		{
			name: "boardV4",
			cidr: "10.16.0.0/16",
			ip:   "10.16.255.255",
			err:  "10.16.255.255 is the broadcast ip in cidr 10.16.0.0/16",
		},
		{
			name: "boardV4",
			cidr: "10.16.0.0/16",
			ip:   "10.16.0.0",
			err:  "10.16.0.0 is the network number ip in cidr 10.16.0.0/16",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := ValidatePodCidr(tt.cidr, tt.ip)
			if !ErrorContains(ret, tt.err) {
				t.Errorf("got %v, want a error %v", ret, tt.err)
			}
		})
	}
}

func TestValidateCidrConflict(t *testing.T) {
	tests := []struct {
		name       string
		subnet     kubeovnv1.Subnet
		subnetList []kubeovnv1.Subnet
		err        string
	}{
		{
			name: "base",
			subnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest0",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.17.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
					Vlan:        "123",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			subnetList: []kubeovnv1.Subnet{
				{
					TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
					ObjectMeta: metav1.ObjectMeta{
						Name: "utest0",
					},
					Spec: kubeovnv1.SubnetSpec{
						Default:     true,
						Vpc:         "ovn-cluster11",
						Protocol:    "IPv4",
						Namespaces:  nil,
						CIDRBlock:   "10.16.0.0/16",
						Gateway:     "10.17.0.1",
						ExcludeIps:  []string{"10.16.0.1"},
						Provider:    "ovn",
						GatewayType: "distributed",
						Vlan:        "1234",
					},
					Status: kubeovnv1.SubnetStatus{},
				},
			},
			err: "",
		},
		{
			name: "cidrOverlapErr",
			subnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest0",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    "IPv4",
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.17.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: "distributed",
					Vlan:        "123",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			subnetList: []kubeovnv1.Subnet{
				{
					TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
					ObjectMeta: metav1.ObjectMeta{
						Name: "utest1",
					},
					Spec: kubeovnv1.SubnetSpec{
						Default:     true,
						Vpc:         "ovn-cluster",
						Protocol:    "IPv4",
						Namespaces:  nil,
						CIDRBlock:   "10.16.0.0/16",
						Gateway:     "10.17.0.1",
						ExcludeIps:  []string{"10.16.0.1"},
						Provider:    "ovn",
						GatewayType: "distributed",
						Vlan:        "123",
					},
					Status: kubeovnv1.SubnetStatus{},
				},
			},
			err: "10.16.0.0/16 is conflict with subnet utest1 cidr 10.16.0.0/16",
		},
		{
			name: "cidrOverlapErr",
			subnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest0",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:               true,
					Vpc:                   "ovn-cluster",
					Protocol:              "IPv4",
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.1"},
					Provider:              "ovn",
					GatewayType:           "distributed",
					Vlan:                  "123",
					ExternalEgressGateway: "12.12.123.12",
					PolicyRoutingTableID:  111,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			subnetList: []kubeovnv1.Subnet{
				{
					TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
					ObjectMeta: metav1.ObjectMeta{
						Name: "utest1",
					},
					Spec: kubeovnv1.SubnetSpec{
						Default:               true,
						Vpc:                   "ovn-cluster",
						Protocol:              "IPv4",
						Namespaces:            nil,
						CIDRBlock:             "10.17.0.0/16",
						Gateway:               "10.17.0.1",
						ExcludeIps:            []string{"10.16.0.1"},
						Provider:              "ovn",
						GatewayType:           "distributed",
						Vlan:                  "123",
						ExternalEgressGateway: "12.12.123.12",
						PolicyRoutingTableID:  111,
					},
					Status: kubeovnv1.SubnetStatus{},
				},
			},
			err: "subnet utest0 policy routing table ID 111 is conflict with subnet utest1 policy routing table ID 111",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := ValidateCidrConflict(tt.subnet, tt.subnetList)
			if !ErrorContains(ret, tt.err) {
				t.Errorf("got %v, want a error", ret)
			}
		})
	}
}
