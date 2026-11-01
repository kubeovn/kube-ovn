package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type errSubnetLister struct{}

func (errSubnetLister) List(labels.Selector) ([]*kubeovnv1.Subnet, error) {
	return nil, errors.New("list failed")
}

func (errSubnetLister) Get(string) (*kubeovnv1.Subnet, error) {
	return nil, errors.New("get failed")
}

func TestGetCidrByProtocol(t *testing.T) {
	cases := []struct {
		name     string
		cidr     string
		protocol string
		wantErr  bool
		expetced string
	}{{
		name:     "ipv4 only",
		cidr:     "1.1.1.0/24",
		protocol: kubeovnv1.ProtocolIPv4,
		expetced: "1.1.1.0/24",
	}, {
		name:     "ipv6 only",
		cidr:     "2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv6,
		expetced: "2001:db8::/120",
	}, {
		name:     "get ipv4 from ipv6",
		cidr:     "2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv4,
	}, {
		name:     "get ipv4 from dual stack",
		cidr:     "1.1.1.0/24,2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv4,
		expetced: "1.1.1.0/24",
	}, {
		name:     "get ipv6 from ipv4",
		cidr:     "1.1.1.0/24",
		protocol: kubeovnv1.ProtocolIPv6,
	}, {
		name:     "get ipv6 from dual stack",
		cidr:     "1.1.1.0/24,2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv6,
		expetced: "2001:db8::/120",
	}, {
		name:     "invalid cidr",
		cidr:     "foo bar",
		protocol: kubeovnv1.ProtocolIPv4,
		wantErr:  true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := getCidrByProtocol(c.cidr, c.protocol)
			if (err != nil) != c.wantErr {
				t.Errorf("getCidrByProtocol(%q, %q) error = %v, wantErr = %v", c.cidr, c.protocol, err, c.wantErr)
			}
			require.Equal(t, c.expetced, got)
		})
	}
}

func TestProviderExistsRequiresSubnetForNamedOvnProvider(t *testing.T) {
	subnets := []*kubeovnv1.Subnet{{
		ObjectMeta: metav1.ObjectMeta{Name: util.DefaultSubnet},
		Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}, {
		ObjectMeta: metav1.ObjectMeta{Name: "attach-subnet"},
		Spec:       kubeovnv1.SubnetSpec{Provider: "attachnet-a.default.ovn"},
	}}

	kubeovnClient := kubeovnfake.NewSimpleClientset()
	informerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnClient, 0)
	subnetInformer := informerFactory.Kubeovn().V1().Subnets()
	for _, subnet := range subnets {
		require.NoError(t, subnetInformer.Informer().GetStore().Add(subnet))
	}

	handler := cniServerHandler{Controller: &Controller{subnetsLister: subnetInformer.Lister()}}

	_, ok := handler.providerExists(util.OvnProvider, "", nil)
	require.True(t, ok)

	subnet, ok := handler.providerExists("attachnet-a.default.ovn", "", nil)
	require.True(t, ok)
	require.NotNil(t, subnet)
	require.Equal(t, "attach-subnet", subnet.Name)

	_, ok = handler.providerExists("attachnet-b.default.ovn", "", nil)
	require.False(t, ok)
}

func TestProviderExistsAllowsOnSubnetListError(t *testing.T) {
	handler := cniServerHandler{Controller: &Controller{subnetsLister: errSubnetLister{}}}

	subnet, ok := handler.providerExists("attachnet-a.default.ovn", "", nil)
	require.True(t, ok)
	require.Nil(t, subnet)
}

func TestProviderExistsAllowsLegacyOvnSubnetSelectedByLogicalSwitch(t *testing.T) {
	const provider = "attachnet.default.ovn"
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy-attachment"},
		Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}
	kubeovnClient := kubeovnfake.NewSimpleClientset()
	informerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnClient, 0)
	subnetInformer := informerFactory.Kubeovn().V1().Subnets()
	require.NoError(t, subnetInformer.Informer().GetStore().Add(subnet))
	handler := cniServerHandler{Controller: &Controller{subnetsLister: subnetInformer.Lister()}}

	annotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnet.Name,
	}
	resolved, ok := handler.providerExists(provider, "", annotations)
	require.True(t, ok)
	require.Equal(t, subnet.Name, resolved.Name)

	interfaceAnnotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider+".net1"): subnet.Name,
	}
	resolved, ok = handler.providerExists(provider, "net1", interfaceAnnotations)
	require.True(t, ok)
	require.Equal(t, subnet.Name, resolved.Name)

	suffixedProvider := provider + ".net1"
	resolved, ok = handler.providerExists(suffixedProvider, "net1", map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, suffixedProvider): subnet.Name,
	})
	require.True(t, ok)
	require.Equal(t, subnet.Name, resolved.Name)
}
