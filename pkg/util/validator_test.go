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
					Protocol:               kubeovnv1.ProtocolIPv4,
					Namespaces:             nil,
					CIDRBlock:              "10.16.0.0/16",
					Gateway:                "10.16.0.1",
					ExcludeIps:             []string{"10.16.0.1"},
					Provider:               OvnProvider,
					GatewayType:            kubeovnv1.GWDistributedType,
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
			name: "GatewayUppercaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-gateway-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv6,
					Namespaces:  nil,
					CIDRBlock:   "2001:db8::/32",
					Gateway:     "2001:Db8::1",
					ExcludeIps:  []string{"2001:db8::a"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet gateway 2001:Db8::1 v6 ip address can not contain upper case",
		},
		{
			name: "CICDblockFormalErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-cicd-block-formal-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "",
					Gateway:     "",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "CIDRBlock:  formal error",
		},
		{
			name: "ExcludeIpsUppercaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-exclude-ips-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv6,
					Namespaces:  nil,
					CIDRBlock:   "2001:db8::/32",
					Gateway:     "2001:db8::1",
					ExcludeIps:  []string{"2001:db8::A"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet exclude ip 2001:db8::A can not contain upper case",
		},
		{
			name: "CidrBlockUppercaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-cidr-block-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv6,
					Namespaces:  nil,
					CIDRBlock:   "2001:Db8::/32",
					Gateway:     "2001:db8::1",
					ExcludeIps:  []string{"2001:db8::a"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet cidr block 2001:Db8::/32 v6 ip address can not contain upper case",
		},
		{
			name: "InvalidZeroCIDRErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-invalid-zero-cidr-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "0.0.0.0",
					Gateway:     "",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "invalid zero cidr \"0.0.0.0\"",
		},
		{
			name: "InvalidCIDRErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-invalid-cidr-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "192.168.1.0/invalid",
					Gateway:     "",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet ut-invalid-cidr-err cidr 192.168.1.0/invalid is invalid",
		},
		{
			name: "ProtocolInvalidErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-protocol-invalid-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:    true,
					Vpc:        "ovn-cluster",
					Protocol:   "ipv5",
					Namespaces: nil,
					CIDRBlock:  "10.16.0.0/16",
					Gateway:    "10.16.0.1",
					ExcludeIps: []string{"10.16.0.1"},
					Provider:   "ovn",

					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "ipv5 is not a valid protocol type",
		},
		{
			name: "SubnetVpcSameNameErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "same-name",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "same-name",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet same-name and vpc same-name cannot have the same name",
		},
		{
			name: "SubnetVpcDifferentNameCorrect",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "subnet-name",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "vpc-name",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "",
		},
		{
			name: "ExternalEgressGatewayUpperCaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-external-egress-gateway-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:               true,
					Vpc:                   "ovn-cluster",
					Protocol:              kubeovnv1.ProtocolIPv6,
					Namespaces:            nil,
					CIDRBlock:             "2001:db8::/32",
					Gateway:               "2001:db8::1",
					ExcludeIps:            []string{"2001:db8::a"},
					Provider:              "ovn",
					ExternalEgressGateway: "2001:dB8::2",
					GatewayType:           kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet ut-external-egress-gateway-uppercase-err external egress gateway 2001:dB8::2 v6 ip address can not contain upper case",
		},
		{
			name: "VipsUpperCaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-vips-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv6,
					Namespaces:  nil,
					CIDRBlock:   "2001:db8::/32",
					Gateway:     "2001:db8::1",
					ExcludeIps:  []string{"2001:db8::a"},
					Provider:    "ovn",
					Vips:        []string{"2001:dB8::2"},
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet ut-vips-uppercase-err vips 2001:dB8::2 v6 ip address can not contain upper case",
		},
		{
			name: "LogicalGatewayU2OInterconnectionSametimeTrueErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-logical-gateway-u2o-interconnection-sametime-true-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:            true,
					Vpc:                "ovn-cluster",
					Protocol:           kubeovnv1.ProtocolIPv6,
					Namespaces:         nil,
					CIDRBlock:          "2001:db8::/32",
					Gateway:            "2001:db8::1",
					ExcludeIps:         []string{"2001:db8::a"},
					Provider:           "ovn",
					GatewayType:        kubeovnv1.GWDistributedType,
					LogicalGateway:     true,
					U2OInterconnection: true,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "logicalGateway and u2oInterconnection can't be opened at the same time",
		},
		{
			name: "ValidateNatOutgoingPolicyRulesErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-validate-nat-outgoing-policy-rules-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv6,
					Namespaces:  nil,
					CIDRBlock:   "2001:db8::/32",
					Gateway:     "2001:db8::1",
					ExcludeIps:  []string{"2001:db8::a"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
					NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRule{
						{
							Match:  kubeovnv1.NatOutGoingPolicyMatch{SrcIPs: "2001:db8::/32,192.168.0.1/24", DstIPs: "2001:db8::/32"},
							Action: "ACCEPT",
						},
					},
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "validate nat policy rules src ips 2001:db8::/32,192.168.0.1/24 failed with err match ips 2001:db8::/32,192.168.0.1/24 protocol is not consistent",
		},
		{
			name: "U2oInterconnectionIpUppercaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-u2o-interconnection-ip-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:              true,
					Vpc:                  "ovn-cluster",
					Protocol:             kubeovnv1.ProtocolIPv6,
					Namespaces:           nil,
					CIDRBlock:            "2001:db8::/32",
					Gateway:              "2001:db8::1",
					ExcludeIps:           []string{"2001:db8::a"},
					Provider:             "ovn",
					GatewayType:          kubeovnv1.GWDistributedType,
					U2OInterconnectionIP: "2001:dB8::2",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet ut-u2o-interconnection-ip-uppercase-err U2O interconnection ip 2001:dB8::2 v6 ip address can not contain upper case",
		},
		{
			name: "U2oInterConnectionIpNotInCidrErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-u2o-interconnection-ip-not-in-cidr-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:              true,
					Vpc:                  "ovn-cluster",
					Protocol:             kubeovnv1.ProtocolIPv6,
					Namespaces:           nil,
					CIDRBlock:            "2001:db8::/32",
					Gateway:              "2001:db8::1",
					ExcludeIps:           []string{"2001:db8::a"},
					Provider:             "ovn",
					GatewayType:          kubeovnv1.GWDistributedType,
					U2OInterconnectionIP: "3001:db8::2",
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "u2oInterconnectionIP 3001:db8::2 is not in subnet ut-u2o-interconnection-ip-not-in-cidr-err cidr 2001:db8::/32",
		},
		{
			name: "GatewayErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-gatewayerr",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.17.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "127.0.0.1/8",
					Gateway:     "127.0.0.1",
					ExcludeIps:  []string{"127.0.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "127.0.1/8",
					Gateway:     "127.0.0.1",
					ExcludeIps:  []string{"127.0.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "127.0.1/8",
					Gateway:     "127.0.0.1",
					ExcludeIps:  []string{"127.0.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1..10.16.0.10..10.16.0.12"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1.."},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "excludeIps is not a valid address",
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.1..10.16.10"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "in excludeIps is not a valid address",
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.2..10.16.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:     kubeovnv1.ProtocolIPv4,
					Namespaces:   nil,
					CIDRBlock:    "10.16.0.0/16",
					Gateway:      "10.16.0.1",
					ExcludeIps:   []string{"10.16.0.1..10.16.0.10"},
					Provider:     OvnProvider,
					GatewayType:  kubeovnv1.GWDistributedType,
					Private:      true,
					AllowSubnets: []string{"10.18.0/16"},
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "10.18.0/16 in allowSubnets is not a valid address",
		},
		{
			name: "AllowSubnetsUppercaseErr",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ut-allow-subnets-uppercase-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:      true,
					Vpc:          "ovn-cluster",
					Protocol:     kubeovnv1.ProtocolIPv6,
					Namespaces:   nil,
					CIDRBlock:    "2001:db8::/32",
					Gateway:      "2001:db8::1",
					ExcludeIps:   []string{"2001:db8::a"},
					Provider:     OvnProvider,
					GatewayType:  kubeovnv1.GWDistributedType,
					Private:      true,
					AllowSubnets: []string{"2001:dB8::/32"},
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "subnet ut-allow-subnets-uppercase-err allow subnet 2001:dB8::/32 v6 ip address can not contain upper case",
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1..10.16.0.10"},
					Provider:    OvnProvider,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.20.0.0/16",
					Gateway:     "10.20.0.1",
					ExcludeIps:  []string{"10.20.0.1..10.20.0.10"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:              kubeovnv1.ProtocolIPv4,
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.1..10.16.0.10"},
					Provider:              OvnProvider,
					GatewayType:           kubeovnv1.GWDistributedType,
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
					Protocol:              kubeovnv1.ProtocolIPv4,
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.2..10.16.0.10"},
					Provider:              OvnProvider,
					GatewayType:           kubeovnv1.GWDistributedType,
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
					Protocol:              kubeovnv1.ProtocolIPv4,
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.2..10.16.0.10"},
					Provider:              OvnProvider,
					GatewayType:           kubeovnv1.GWDistributedType,
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
					Protocol:              kubeovnv1.ProtocolIPv4,
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.1"},
					Provider:              OvnProvider,
					GatewayType:           kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1..10.16.0.10"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.16.0.1",
					ExcludeIps:  []string{"10.16.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "ip 10.16.1 in excludeIps is not a valid address",
		},
		{
			name: "ValidPTPSubnet",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-ptpsubnet",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/31",
					Gateway:     "10.16.0.0",
					ExcludeIps:  []string{"10.16.0.0"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "",
		},
		{
			name: "Invalid/32Subnet",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-ptpsubnet",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:     true,
					Vpc:         "ovn-cluster",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/32",
					Gateway:     "10.16.0.0",
					ExcludeIps:  []string{"10.16.0.0"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "validate gateway 10.16.0.0 for cidr 10.16.0.0/32 failed: subnet 10.16.0.0/32 is configured with /32 or /128 netmask",
		},
		{
			name: "NonOvnSubnetArbitraryGatewayWithDisableGatewayCheck",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-nonovn-arbitrary-gw",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:             false,
					Vpc:                 "ovn-cluster",
					Protocol:            kubeovnv1.ProtocolIPv4,
					Namespaces:          nil,
					CIDRBlock:           "10.16.0.0/16",
					Gateway:             "192.168.1.1",
					ExcludeIps:          []string{"10.16.0.1"},
					Provider:            "custom-provider",
					GatewayType:         kubeovnv1.GWDistributedType,
					DisableGatewayCheck: true,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "",
		},
		{
			name: "NonOvnSubnetArbitraryGatewayWithoutDisableGatewayCheckShouldFail",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-nonovn-arbitrary-gw-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:             false,
					Vpc:                 "ovn-cluster",
					Protocol:            kubeovnv1.ProtocolIPv4,
					Namespaces:          nil,
					CIDRBlock:           "10.16.0.0/16",
					Gateway:             "192.168.1.1",
					ExcludeIps:          []string{"10.16.0.1"},
					Provider:            "custom-provider",
					GatewayType:         kubeovnv1.GWDistributedType,
					DisableGatewayCheck: false,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "gateway 192.168.1.1 is not in cidr 10.16.0.0/16",
		},
		{
			name: "OvnSubnetArbitraryGatewayShouldFailEvenWithDisableGatewayCheck",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-ovn-arbitrary-gw-err",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:             false,
					Vpc:                 "ovn-cluster",
					Protocol:            kubeovnv1.ProtocolIPv4,
					Namespaces:          nil,
					CIDRBlock:           "10.16.0.0/16",
					Gateway:             "192.168.1.1",
					ExcludeIps:          []string{"10.16.0.1"},
					Provider:            OvnProvider,
					GatewayType:         kubeovnv1.GWDistributedType,
					DisableGatewayCheck: true,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "gateway 192.168.1.1 is not in cidr 10.16.0.0/16",
		},
		{
			name: "NonOvnSubnetArbitraryGatewayIPv6WithDisableGatewayCheck",
			asubnet: kubeovnv1.Subnet{
				TypeMeta: metav1.TypeMeta{Kind: "Subnet", APIVersion: "kubeovn.io/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "utest-nonovn-arbitrary-gw-ipv6",
				},
				Spec: kubeovnv1.SubnetSpec{
					Default:             false,
					Vpc:                 "ovn-cluster",
					Protocol:            kubeovnv1.ProtocolIPv6,
					Namespaces:          nil,
					CIDRBlock:           "2001:db8::/32",
					Gateway:             "2001:db9::1",
					ExcludeIps:          []string{"2001:db8::a"},
					Provider:            "custom-provider",
					GatewayType:         kubeovnv1.GWDistributedType,
					DisableGatewayCheck: true,
				},
				Status: kubeovnv1.SubnetStatus{},
			},
			err: "",
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
			err: "10.16.1111.15 in ovn.kubernetes.io/ip_pool is not a valid address",
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
			t.Logf("test case %v", tt.name)
			ret := ValidatePodNetwork(tt.annotations)
			if !ErrorContains(ret, tt.err) {
				t.Errorf("got %v, want a error %v", ret, tt.err)
			}
		})
	}
}

func TestValidateNetworkBroadcast(t *testing.T) {
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
		{
			name: "boardV4/31subnet",
			cidr: "10.16.0.0/31",
			ip:   "",
			err:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := ValidateNetworkBroadcast(tt.cidr, tt.ip)
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.17.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
						Protocol:    kubeovnv1.ProtocolIPv4,
						Namespaces:  nil,
						CIDRBlock:   "10.16.0.0/16",
						Gateway:     "10.17.0.1",
						ExcludeIps:  []string{"10.16.0.1"},
						Provider:    OvnProvider,
						GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:    kubeovnv1.ProtocolIPv4,
					Namespaces:  nil,
					CIDRBlock:   "10.16.0.0/16",
					Gateway:     "10.17.0.1",
					ExcludeIps:  []string{"10.16.0.1"},
					Provider:    OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
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
						Protocol:    kubeovnv1.ProtocolIPv4,
						Namespaces:  nil,
						CIDRBlock:   "10.16.0.0/16",
						Gateway:     "10.17.0.1",
						ExcludeIps:  []string{"10.16.0.1"},
						Provider:    OvnProvider,
						GatewayType: kubeovnv1.GWDistributedType,
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
					Protocol:              kubeovnv1.ProtocolIPv4,
					Namespaces:            nil,
					CIDRBlock:             "10.16.0.0/16",
					Gateway:               "10.16.0.1",
					ExcludeIps:            []string{"10.16.0.1"},
					Provider:              OvnProvider,
					GatewayType:           kubeovnv1.GWDistributedType,
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
						Protocol:              kubeovnv1.ProtocolIPv4,
						Namespaces:            nil,
						CIDRBlock:             "10.17.0.0/16",
						Gateway:               "10.17.0.1",
						ExcludeIps:            []string{"10.16.0.1"},
						Provider:              OvnProvider,
						GatewayType:           kubeovnv1.GWDistributedType,
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

func TestValidateNatOutgoingPolicyRules(t *testing.T) {
	tests := []struct {
		name   string
		subnet kubeovnv1.Subnet
		err    string
	}{
		{
			name: "valid rules",
			subnet: kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{
					NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRule{
						{
							Match: kubeovnv1.NatOutGoingPolicyMatch{
								SrcIPs: "10.0.0.0/24",
								DstIPs: "192.168.0.0/16",
							},
						},
					},
				},
			},
			err: "",
		},
		{
			name: "invalid src ips",
			subnet: kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{
					NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRule{
						{
							Match: kubeovnv1.NatOutGoingPolicyMatch{
								SrcIPs: "invalid",
								DstIPs: "192.168.0.0/16",
							},
						},
					},
				},
			},
			err: "validate nat policy rules src ips invalid failed with err",
		},
		{
			name: "invalid dst ips",
			subnet: kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{
					NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRule{
						{
							Match: kubeovnv1.NatOutGoingPolicyMatch{
								SrcIPs: "10.0.0.0/24",
								DstIPs: "invalid",
							},
						},
					},
				},
			},
			err: "validate nat policy rules dst ips invalid failed with err",
		},
		{
			name: "mismatched protocols",
			subnet: kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{
					NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRule{
						{
							Match: kubeovnv1.NatOutGoingPolicyMatch{
								SrcIPs: "10.0.0.0/24",
								DstIPs: "2001:db8::/64",
							},
						},
					},
				},
			},
			err: "Match.SrcIPS protocol IPv4 not equal to Match.DstIPs protocol IPv6",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNatOutgoingPolicyRules(tc.subnet)
			if !ErrorContains(err, tc.err) {
				t.Errorf("Expected error containing %q, got %v", tc.err, err)
			}
		})
	}
}

func TestValidateVpc(t *testing.T) {
	tests := []struct {
		name    string
		vpc     *kubeovnv1.Vpc
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid vpc",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					StaticRoutes: []*kubeovnv1.StaticRoute{
						{
							CIDR:      "192.168.0.0/24",
							NextHopIP: "10.0.0.1",
						},
					},
					PolicyRoutes: []*kubeovnv1.PolicyRoute{
						{
							Action:    kubeovnv1.PolicyRouteActionAllow,
							NextHopIP: "10.0.0.1",
						},
					},
					VpcPeerings: []*kubeovnv1.VpcPeering{
						{
							LocalConnectIP: "192.168.1.0/24",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid static route policy",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					StaticRoutes: []*kubeovnv1.StaticRoute{
						{
							CIDR:      "192.168.0.0/24",
							NextHopIP: "10.0.0.1",
							Policy:    "invalid",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid vpc static route CIDR",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					StaticRoutes: []*kubeovnv1.StaticRoute{
						{
							CIDR:      "192.168.%.0/24",
							NextHopIP: "10.0.0.1",
						},
					},
					PolicyRoutes: []*kubeovnv1.PolicyRoute{
						{
							Action:    kubeovnv1.PolicyRouteActionAllow,
							NextHopIP: "10.0.0.1",
						},
					},
					VpcPeerings: []*kubeovnv1.VpcPeering{
						{
							LocalConnectIP: "192.168.1.0/24",
						},
					},
				},
			},

			wantErr: true,
			errMsg:  "invalid cidr 192.168.%.0/24: invalid CIDR address: 192.168.%.0/24",
		},
		{
			name: "invalid static route CIDR",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					StaticRoutes: []*kubeovnv1.StaticRoute{
						{
							CIDR:      "invalid",
							NextHopIP: "10.0.0.1",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid static route NextHopIP",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					StaticRoutes: []*kubeovnv1.StaticRoute{
						{
							CIDR:      "192.168.0.0/24",
							NextHopIP: "invalid",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid policy route action",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					PolicyRoutes: []*kubeovnv1.PolicyRoute{
						{
							Action:    "invalid",
							NextHopIP: "10.0.0.1",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid policy route NextHopIP",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					PolicyRoutes: []*kubeovnv1.PolicyRoute{
						{
							Action:    kubeovnv1.PolicyRouteActionReroute,
							NextHopIP: "invalid",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid vpc peering LocalConnectIP",
			vpc: &kubeovnv1.Vpc{
				Spec: kubeovnv1.VpcSpec{
					VpcPeerings: []*kubeovnv1.VpcPeering{
						{
							LocalConnectIP: "invalid",
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVpc(tt.vpc)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error = %v, but wantErr %v", err, tt.wantErr)
			}
			if tt.errMsg != "" && err != nil && err.Error() != tt.errMsg {
				t.Errorf("expected error message %q, but got %q", tt.errMsg, err.Error())
			}
		})
	}
}

func TestValidateNatOutGoingPolicyRuleIPs(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		expectErr bool
	}{
		{
			name:      "Valid IPv4",
			input:     "192.168.1.1,10.0.0.1",
			want:      "IPv4",
			expectErr: false,
		},
		{
			name:      "Valid IPv6",
			input:     "2001:0db8::1,2001:0db8::2",
			want:      "IPv6",
			expectErr: false,
		},
		{
			name:      "Mixed IPv4 and IPv6",
			input:     "192.168.1.1,2001:0db8::1",
			want:      "",
			expectErr: true,
		},
		{
			name:      "Invalid IP",
			input:     "invalid_ip",
			want:      "",
			expectErr: true,
		},
		{
			name:      "Empty string",
			input:     "",
			want:      "",
			expectErr: true,
		},
		{
			name:      "Valid CIDR",
			input:     "192.168.1.0/24,10.0.0.0/8",
			want:      "IPv4",
			expectErr: false,
		},
		{
			name:      "Mixed IP and CIDR",
			input:     "192.168.1.1,10.0.0.0/8",
			want:      "IPv4",
			expectErr: false,
		},
		{
			name:      "Invalid CIDR",
			input:     "192.168.1.0/33",
			want:      "",
			expectErr: true,
		},
		{
			name:      "Single IPv4",
			input:     "10.0.0.1",
			want:      "IPv4",
			expectErr: false,
		},
		{
			name:      "Single IPv6",
			input:     "2001:0db8::1",
			want:      "IPv6",
			expectErr: false,
		},
		{
			name:      "Single Invalid IP",
			input:     "300.300.300.300",
			want:      "",
			expectErr: true,
		},
		{
			name:      "Empty after split",
			input:     ",",
			want:      "",
			expectErr: true,
		},

		{
			name:      "Valid CIDR with IPv6",
			input:     "192.168.1.0/24,2001:0db8::1",
			want:      "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateNatOutGoingPolicyRuleIPs(tt.input)
			if (err != nil) != tt.expectErr {
				t.Errorf("validateNatOutGoingPolicyRuleIPs() error = %v, wantErr %v", err, tt.expectErr)
				return
			}
			if got != tt.want {
				t.Errorf("validateNatOutGoingPolicyRuleIPs() = %v, want %v", got, tt.want)
			}
		})
	}
}
