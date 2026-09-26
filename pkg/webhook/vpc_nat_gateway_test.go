package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type mockCache struct {
	objects   map[string]runtime.Object
	dnatRules []ovnv1.IptablesDnatRule
}

func (m *mockCache) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	o, ok := m.objects[key.String()]
	if !ok {
		return k8serrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	switch t := obj.(type) {
	case *ovnv1.Vpc:
		*t = *o.(*ovnv1.Vpc)
	case *ovnv1.Subnet:
		*t = *o.(*ovnv1.Subnet)
	case *corev1.ConfigMap:
		*t = *o.(*corev1.ConfigMap)
	case *ovnv1.QoSPolicy:
		*t = *o.(*ovnv1.QoSPolicy)
	case *ovnv1.IptablesEIP:
		*t = *o.(*ovnv1.IptablesEIP)
	case *ovnv1.VpcNatGateway:
		*t = *o.(*ovnv1.VpcNatGateway)
	default:
		return fmt.Errorf("unsupported type in mock cache: %T", obj)
	}

	return nil
}

func (m *mockCache) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// The validation code lists every share DNAT rule only for the unlabelled owner/affinity
	// conflict check; labelled lookups (which use ListOptions) keep returning an empty result
	// so existing tests that do not exercise the new check stay unchanged.
	if len(opts) == 0 {
		if dnats, ok := list.(*ovnv1.IptablesDnatRuleList); ok {
			dnats.Items = m.dnatRules
		}
	}
	return nil
}

func (m *mockCache) GetInformer(_ context.Context, _ client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	return nil, nil
}

func (m *mockCache) GetInformerForKind(_ context.Context, _ schema.GroupVersionKind, _ ...cache.InformerGetOption) (cache.Informer, error) {
	return nil, nil
}

func (m *mockCache) RemoveInformer(_ context.Context, _ client.Object) error {
	return nil
}

func (m *mockCache) Start(_ context.Context) error {
	return nil
}

func (m *mockCache) WaitForCacheSync(_ context.Context) bool {
	return true
}

func (m *mockCache) IndexField(_ context.Context, _ client.Object, _ string, _ client.IndexerFunc) error {
	return nil
}

func TestVpcNatGwCreateOrUpdateHook(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = ovnv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	decoder := admission.NewDecoder(scheme)

	t.Run("Create - Allowed", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: gwRaw,
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{
					Name: "test-vpc",
					Spec: ovnv1.VpcSpec{EnableBfd: true},
				},
				"/test-subnet": &ovnv1.Subnet{
					Name: "test-subnet",
					Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
				},
			},
		}

		v := &ValidatingHook{
			decoder: decoder,
			cache:   cache,
		}

		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.True(t, resp.Allowed, "Expected allowed, got reason: %+v", resp.Result)
	})

	t.Run("Create - Invalid LanIP", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "invalid-ip",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: gwRaw},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{Name: "test-vpc"},
				"/test-subnet": &ovnv1.Subnet{
					Name: "test-subnet",
					Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
				},
			},
		}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is not a valid IP")
	})

	t.Run("Update - Immutable Namespace", func(t *testing.T) {
		gwOld := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Namespace: "old-ns"},
		}
		gwNew := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Namespace: "new-ns"},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: gwNewRaw},
			OldObject: runtime.RawExtension{Raw: gwOldRaw},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "spec.namespace is immutable")
	})

	t.Run("Update - Immutable VPC", func(t *testing.T) {
		gwOld := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Vpc: "old-vpc"},
		}
		gwNew := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Vpc: "new-vpc"},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: gwNewRaw},
			OldObject: runtime.RawExtension{Raw: gwOldRaw},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "spec.vpc is immutable")
	})

	t.Run("Update - HA to non-HA Reduction", func(t *testing.T) {
		gwOld := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Replicas: 2},
		}
		gwNew := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Replicas: 1},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: gwNewRaw},
			OldObject: runtime.RawExtension{Raw: gwOldRaw},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "replica count reduction from HA (>1) to non-HA (1) is not supported")
	})

	t.Run("Update - non-HA to HA Increase", func(t *testing.T) {
		gwOld := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Replicas: 1},
		}
		gwNew := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Replicas: 2},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: gwNewRaw},
			OldObject: runtime.RawExtension{Raw: gwOldRaw},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "replica count increase from non-HA (1) to HA (>1) is not supported")
	})

	t.Run("ConfigMap not found", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: gwRaw},
		}

		cache := &mockCache{objects: map[string]runtime.Object{}}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "configMap \"ovn-vpc-nat-config\" not configured")
	})

	t.Run("VpcNatGatewayConfig disabled", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: gwRaw},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"enable-vpc-nat-gw": "false"},
				},
			},
		}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "parameter \"enable-vpc-nat-gw\" in ConfigMap \"ovn-vpc-nat-gw-config\" not true")
	})

	t.Run("ValidateVpcNatGW - missing VPC", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "missing-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: gwRaw},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"enable-vpc-nat-gw": "true"},
				},
			},
		}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "missing-vpc")
	})

	t.Run("ValidateVpcNatGW - BFD mismatch", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
				BFD:      ovnv1.VpcNatGatewayBFDConfig{Enabled: true},
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: gwRaw},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{
					Name: "test-vpc",
					Spec: ovnv1.VpcSpec{EnableBfd: false},
				},
				"/test-subnet": &ovnv1.Subnet{
					Name: "test-subnet",
					Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
				},
			},
		}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "BFD is enabled on NAT gateway but VPC test-vpc does not have BFD enabled")
	})

	t.Run("ValidateVpcNatGW - LanIP not in CIDR", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "192.168.1.1",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: gwRaw},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
					Data: map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{Name: "test-vpc"},
				"/test-subnet": &ovnv1.Subnet{
					Name: "test-subnet",
					Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
				},
			},
		}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is not in the range of subnet test-subnet")
	})

	t.Run("Decoding failure", func(t *testing.T) {
		req := admission.Request{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: []byte("invalid-json")},
		}
		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Equal(t, int32(http.StatusBadRequest), resp.Result.Code)
	})
}

func TestValidateIptablesDnat(t *testing.T) {
	newDnat := func(externalPort, internalPort string) *ovnv1.IptablesDnatRule {
		return &ovnv1.IptablesDnatRule{
			Name: "test-dnat",
			Spec: ovnv1.IptablesDnatRuleSpec{
				EIP:          "test-eip",
				ExternalPort: externalPort,
				InternalPort: internalPort,
				InternalIP:   "10.0.0.10",
				Protocol:     "tcp",
			},
		}
	}

	cache := &mockCache{
		objects: map[string]runtime.Object{
			"/test-eip": &ovnv1.IptablesEIP{
				Name: "test-eip",
				Spec: ovnv1.IptablesEIPSpec{V4ip: "192.168.0.1"},
			},
		},
	}
	v := &ValidatingHook{cache: cache}

	tests := []struct {
		name         string
		externalPort string
		internalPort string
		wantErr      bool
	}{
		{name: "valid ports", externalPort: "8080", internalPort: "80", wantErr: false},
		{name: "min valid port", externalPort: "1", internalPort: "1", wantErr: false},
		{name: "max valid port", externalPort: "65535", internalPort: "65535", wantErr: false},
		{name: "external port zero rejected", externalPort: "0", internalPort: "80", wantErr: true},
		{name: "internal port zero rejected", externalPort: "8080", internalPort: "0", wantErr: true},
		{name: "external port over range rejected", externalPort: "65536", internalPort: "80", wantErr: true},
		{name: "external port non-numeric rejected", externalPort: "abc", internalPort: "80", wantErr: true},
		{name: "external port leading zero rejected", externalPort: "080", internalPort: "80", wantErr: true},
		{name: "internal port leading zero rejected", externalPort: "8080", internalPort: "080", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateIptablesDnat(context.Background(), newDnat(tt.externalPort, tt.internalPort))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateIptablesDnatProtocolCanonical(t *testing.T) {
	v := &ValidatingHook{cache: &mockCache{objects: map[string]runtime.Object{
		"/test-eip": &ovnv1.IptablesEIP{Name: "test-eip", Spec: ovnv1.IptablesEIPSpec{V4ip: "192.168.0.1"}},
	}}}

	for _, protocol := range []string{"TCP", "Udp"} {
		dnat := &ovnv1.IptablesDnatRule{Spec: ovnv1.IptablesDnatRuleSpec{
			EIP: "test-eip", ExternalPort: "80", InternalPort: "80", InternalIP: "10.0.0.10", Protocol: protocol,
		}}
		require.Error(t, v.ValidateIptablesDnat(context.Background(), dnat), protocol)
	}
}

func TestValidateIptablesDnatSessionAffinity(t *testing.T) {
	cache := &mockCache{
		objects: map[string]runtime.Object{
			"/test-eip": &ovnv1.IptablesEIP{
				Name: "test-eip",
				Spec: ovnv1.IptablesEIPSpec{V4ip: "192.168.0.1"},
			},
		},
	}
	v := &ValidatingHook{cache: cache}

	base := &ovnv1.IptablesDnatRule{
		Name: "test-dnat",
		Spec: ovnv1.IptablesDnatRuleSpec{
			Type:         ovnv1.DnatRuleTypeShare,
			EIP:          "test-eip",
			ExternalPort: "80",
			InternalPort: "8080",
			InternalIP:   "10.0.0.10",
			Protocol:     "tcp",
		},
	}

	tests := []struct {
		name    string
		mutate  func(spec *ovnv1.IptablesDnatRuleSpec)
		wantErr bool
	}{
		{
			name: "none with zero timeout is valid",
		},
		{
			name: "clientip with zero timeout is valid",
			mutate: func(spec *ovnv1.IptablesDnatRuleSpec) {
				spec.SessionAffinity = ovnv1.DnatSessionAffinityClientIP
			},
		},
		{
			name: "clientip with timeout is valid",
			mutate: func(spec *ovnv1.IptablesDnatRuleSpec) {
				spec.SessionAffinity = ovnv1.DnatSessionAffinityClientIP
				spec.SessionAffinityTimeoutSeconds = 600
			},
		},
		{
			name: "none with timeout is rejected",
			mutate: func(spec *ovnv1.IptablesDnatRuleSpec) {
				spec.SessionAffinityTimeoutSeconds = 600
			},
			wantErr: true,
		},
		{
			name: "clientip on exclusive is rejected",
			mutate: func(spec *ovnv1.IptablesDnatRuleSpec) {
				spec.Type = ovnv1.DnatRuleTypeExclusive
				spec.SessionAffinity = ovnv1.DnatSessionAffinityClientIP
			},
			wantErr: true,
		},
		{
			name: "clientip timeout out of range is rejected",
			mutate: func(spec *ovnv1.IptablesDnatRuleSpec) {
				spec.SessionAffinity = ovnv1.DnatSessionAffinityClientIP
				spec.SessionAffinityTimeoutSeconds = 86401
			},
			wantErr: true,
		},
		{
			name: "unknown affinity is rejected",
			mutate: func(spec *ovnv1.IptablesDnatRuleSpec) {
				spec.SessionAffinity = "PerConnection"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dnat := base.DeepCopy()
			if tt.mutate != nil {
				tt.mutate(&dnat.Spec)
			}
			err := v.ValidateIptablesDnat(context.Background(), dnat)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIptablesDnatAffinityImmutableOnUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ovnv1.AddToScheme(scheme))

	encode := func(dnat *ovnv1.IptablesDnatRule) runtime.RawExtension {
		raw, err := json.Marshal(dnat)
		require.NoError(t, err)
		return runtime.RawExtension{Raw: raw}
	}

	oldDnat := &ovnv1.IptablesDnatRule{
		Name: "test-dnat",
		Spec: ovnv1.IptablesDnatRuleSpec{
			Type:         ovnv1.DnatRuleTypeShare,
			EIP:          "test-eip",
			ExternalPort: "80",
			InternalPort: "8080",
			InternalIP:   "10.0.0.1",
			Protocol:     "tcp",
		},
	}
	newDnat := oldDnat.DeepCopy()
	newDnat.Spec.SessionAffinity = ovnv1.DnatSessionAffinityClientIP
	newDnat.Spec.SessionAffinityTimeoutSeconds = 600

	req := admission.Request{
		Operation: admissionv1.Update,
		OldObject: encode(oldDnat),
		Object:    encode(newDnat),
	}
	v := &ValidatingHook{decoder: admission.NewDecoder(scheme)}
	resp := v.iptablesDnatUpdateHook(context.Background(), req)
	require.False(t, resp.Allowed)
	require.Contains(t, resp.Result.Message, "sessionAffinity is immutable")
}

// TestValidateIptablesDnatClusterIPFlow pins the webhook contract of the two VIP flows: a rule
// addresses one or both VIPs, and a ClusterIP-only record uses its gateway label because there is
// no EIP to derive it from.
func TestValidateIptablesDnatClusterIPFlow(t *testing.T) {
	t.Parallel()

	cache := &mockCache{objects: map[string]runtime.Object{
		"/test-eip": &ovnv1.IptablesEIP{Name: "test-eip", Spec: ovnv1.IptablesEIPSpec{V4ip: "192.168.0.1", NatGwDp: "gw1"}},
		"/gw0":      &ovnv1.VpcNatGateway{Name: "gw0"},
		"/gw1":      &ovnv1.VpcNatGateway{Name: "gw1"},
		"/gw-gone":  &ovnv1.VpcNatGateway{Name: "gw-gone"},
	}}
	v := &ValidatingHook{cache: cache}

	base := func() *ovnv1.IptablesDnatRule {
		return &ovnv1.IptablesDnatRule{
			Name: "test-dnat",
			Spec: ovnv1.IptablesDnatRuleSpec{
				ExternalPort: "80", InternalPort: "8080",
				InternalIP: "10.0.0.10", Protocol: "tcp",
				Type: ovnv1.DnatRuleTypeShare,
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(d *ovnv1.IptablesDnatRule)
		wantErr string
	}{
		{
			name: "clusterIP with an explicit gateway label is accepted",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.ClusterIP = "10.96.1.5"
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw0"}
			},
		},
		{
			name:    "clusterIP without a gateway label is rejected",
			mutate:  func(d *ovnv1.IptablesDnatRule) { d.Spec.ClusterIP = "10.96.1.5" },
			wantErr: "gateway label is required with clusterIP when there is no eip",
		},
		{
			// A Service handled by the nftable LB service feature carries its ingress IP and its
			// ClusterIP aligned on one rule, so both fields together is the normal case there.
			name: "eip and clusterIP together are accepted",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.EIP = "test-eip"
				d.Spec.ClusterIP = "10.96.1.5"
			},
		},
		{
			name: "clusterIP with a gateway label that does not serve the eip is rejected",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.EIP = "test-eip"
				d.Spec.ClusterIP = "10.96.1.5"
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw0"}
			},
			wantErr: "does not serve eip",
		},
		{
			name: "clusterIP with a missing gateway label target is rejected",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.ClusterIP = "10.96.1.5"
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "absent"}
			},
			wantErr: "not found",
		},
		{
			name: "clusterIP must be an IPv4 address",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.ClusterIP = "fd00::1"
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw0"}
			},
			wantErr: "must be an IPv4 address",
		},
		{
			name: "clusterIP is not accepted on an exclusive rule",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.ClusterIP = "10.96.1.5"
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw0"}
				d.Spec.Type = ""
			},
			wantErr: "clusterIP requires type=share",
		},
		{
			name:    "neither eip nor clusterIP is rejected",
			mutate:  func(*ovnv1.IptablesDnatRule) {},
			wantErr: `"eip" or "clusterIP" cannot be empty`,
		},
		{
			// An EIP plus ClusterIP record identifies its gateway with the controller-owned label.
			name: "gateway label matching the eip gateway is accepted",
			mutate: func(d *ovnv1.IptablesDnatRule) {
				d.Spec.EIP = "test-eip"
				d.Spec.ClusterIP = "10.96.1.5"
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw1"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dnat := base()
			tt.mutate(dnat)
			err := v.ValidateIptablesDnat(context.Background(), dnat)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestValidateIptablesDnatRejectsCrossOwnerClusterIPIdentity pins that a rule carrying both an EIP
// and a ClusterIP contributes two identities: two rules with different EIPs but the same internal
// VIP and port still share one nft map, so they must belong to the same owner. Without this the
// Service's ClusterIP traffic would reach the backends of a hand-managed rule.
func TestNftableLbOwnerLabelsAreControllerOnly(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, ovnv1.AddToScheme(scheme))
	encode := func(dnat *ovnv1.IptablesDnatRule) runtime.RawExtension {
		raw, err := json.Marshal(dnat)
		require.NoError(t, err)
		return runtime.RawExtension{Raw: raw}
	}
	const controller = "system:serviceaccount:kube-system:ovn"

	owned := &ovnv1.IptablesDnatRule{
		Name: "test-dnat",
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:     "ns1",
			util.NftableLbSvcNameLabel:   "svc",
			util.NftableLbSvcUIDLabel:    "uid-1",
			util.NftableLbSvcRecordLabel: "true",
		},
		Spec: ovnv1.IptablesDnatRuleSpec{
			Type: ovnv1.DnatRuleTypeShare, EIP: "test-eip",
			ExternalPort: "80", InternalPort: "8080", InternalIP: "10.0.0.1", Protocol: "tcp",
		},
	}
	unowned := owned.DeepCopy()
	unowned.Labels = nil

	v := &ValidatingHook{
		decoder: admission.NewDecoder(scheme),
		cache: &mockCache{objects: map[string]runtime.Object{
			"/test-eip": &ovnv1.IptablesEIP{Name: "test-eip", Spec: ovnv1.IptablesEIPSpec{V4ip: "192.168.0.1"}},
		}},
		controllerIdentity: controller,
	}

	t.Run("manual share create is rejected", func(t *testing.T) {
		manual := owned.DeepCopy()
		manual.Labels = nil
		resp := v.iptablesDnatCreateHook(context.Background(), admission.Request{
			Operation: admissionv1.Create,
			Object:    encode(manual),
			UserInfo:  authenticationv1.UserInfo{Username: controller},
		})
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "reserved for nftable LB Service accounting records")
	})

	t.Run("create with forged owner labels is rejected", func(t *testing.T) {
		resp := v.iptablesDnatCreateHook(context.Background(), admission.Request{
			Operation: admissionv1.Create,
			Object:    encode(owned),
			UserInfo:  authenticationv1.UserInfo{Username: "system:serviceaccount:default:attacker"},
		})
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "owned by the kube-ovn controller")
	})

	t.Run("owner labels added by an update are rejected", func(t *testing.T) {
		resp := v.iptablesDnatUpdateHook(context.Background(), admission.Request{
			Operation: admissionv1.Update,
			OldObject: encode(unowned),
			Object:    encode(owned),
			UserInfo:  authenticationv1.UserInfo{Username: "system:serviceaccount:default:attacker"},
		})
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "owned by the kube-ovn controller")
	})

	t.Run("an unrelated metadata update by any user is allowed", func(t *testing.T) {
		annotated := owned.DeepCopy()
		annotated.Annotations = map[string]string{"example.com/note": "hi"}
		resp := v.iptablesDnatUpdateHook(context.Background(), admission.Request{
			Operation: admissionv1.Update,
			OldObject: encode(owned),
			Object:    encode(annotated),
			UserInfo:  authenticationv1.UserInfo{Username: "system:serviceaccount:default:attacker"},
		})
		require.True(t, resp.Allowed)
	})

	t.Run("the controller may write them", func(t *testing.T) {
		resp := v.iptablesDnatUpdateHook(context.Background(), admission.Request{
			Operation: admissionv1.Update,
			OldObject: encode(unowned),
			Object:    encode(owned),
			UserInfo:  authenticationv1.UserInfo{Username: controller},
		})
		require.True(t, resp.Allowed)
	})
}

// TestValidateIptablesDnatOwnerIncarnation pins that ownership is compared per Service incarnation:
// a rule left behind by a deleted Service must not share an identity with a new Service of the same
// name, because one nft map would then aggregate both Services' backends.
func TestVpcNatGwDeleteHookBlocksClusterIPRules(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, ovnv1.AddToScheme(scheme))

	clusterIPRule := &ovnv1.IptablesDnatRule{
		Name: "lb-web-abc", Labels: map[string]string{util.VpcNatGatewayNameLabel: "gw0"},
		Spec: ovnv1.IptablesDnatRuleSpec{
			Type: ovnv1.DnatRuleTypeShare, ClusterIP: "10.96.1.5",
			ExternalPort: "80", InternalPort: "8080", InternalIP: "10.0.7.2", Protocol: "tcp",
		},
	}
	req := admission.Request{Operation: admissionv1.Delete, Name: "gw0"}

	hook := func(objects ...client.Object) *ValidatingHook {
		return &ValidatingHook{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
	}

	resp := hook(clusterIPRule).VpcNatGwDeleteHook(context.Background(), req)
	require.False(t, resp.Allowed)
	require.Contains(t, resp.Result.Message, "serves clusterIP 10.96.1.5")

	// Another gateway's rule, and a rule reached through an EIP (covered by the EIP check), do not block.
	otherGw := clusterIPRule.DeepCopy()
	otherGw.Name, otherGw.Labels[util.VpcNatGatewayNameLabel] = "lb-web-other", "gw1"
	eipRule := clusterIPRule.DeepCopy()
	eipRule.Name, eipRule.Spec.EIP = "lb-web-eip", "eip0"
	require.True(t, hook(otherGw, eipRule).VpcNatGwDeleteHook(context.Background(), req).Allowed)

	// A rule that is already terminating must not block the gateway forever.
	terminating := clusterIPRule.DeepCopy()
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"kubeovn.io/keep"}
	require.True(t, hook(terminating).VpcNatGwDeleteHook(context.Background(), req).Allowed)
}

// TestNftableLbOwnerLabelsCheckDisabled pins the escape hatch: a deployment whose controller
// authenticates as a user this webhook cannot be told about disables the check with an empty
// identity, instead of locking the controller out of its own labels.
func TestNftableLbOwnerLabelsCheckDisabled(t *testing.T) {
	t.Parallel()

	v := &ValidatingHook{}
	require.NoError(t, v.validateNftableLbOwnerLabels(
		admission.Request{
			UserInfo: authenticationv1.UserInfo{Username: "system:serviceaccount:default:anyone"},
		},
		nil, map[string]string{util.NftableLbSvcNsLabel: "ns1", util.NftableLbSvcNameLabel: "svc"},
	))
}
