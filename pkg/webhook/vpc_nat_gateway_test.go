package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type mockCache struct {
	objects map[string]runtime.Object
}

func (m *mockCache) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	o, ok := m.objects[key.String()]
	if !ok {
		return k8serrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	switch t := obj.(type) {
	case *ovnv1.Vpc:
		*t = *(o.(*ovnv1.Vpc))
	case *ovnv1.Subnet:
		*t = *(o.(*ovnv1.Subnet))
	case *corev1.ConfigMap:
		*t = *(o.(*corev1.ConfigMap))
	case *ovnv1.QoSPolicy:
		*t = *(o.(*ovnv1.QoSPolicy))
	default:
		return fmt.Errorf("unsupported type in mock cache: %T", obj)
	}

	return nil
}

func (m *mockCache) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
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
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-gw",
			},
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object: runtime.RawExtension{
					Raw: gwRaw,
				},
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{
					ObjectMeta: metav1.ObjectMeta{Name: "test-vpc"},
					Spec:       ovnv1.VpcSpec{EnableBfd: true},
				},
				"/test-subnet": &ovnv1.Subnet{
					ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
					Spec:       ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "invalid-ip",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: gwRaw},
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{ObjectMeta: metav1.ObjectMeta{Name: "test-vpc"}},
				"/test-subnet": &ovnv1.Subnet{
					ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
					Spec:       ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec:       ovnv1.VpcNatGatewaySpec{Namespace: "old-ns"},
		}
		gwNew := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec:       ovnv1.VpcNatGatewaySpec{Namespace: "new-ns"},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Update,
				Object:    runtime.RawExtension{Raw: gwNewRaw},
				OldObject: runtime.RawExtension{Raw: gwOldRaw},
			},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "spec.namespace is immutable")
	})

	t.Run("Update - HA to non-HA Reduction", func(t *testing.T) {
		gwOld := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec:       ovnv1.VpcNatGatewaySpec{Replicas: 2},
		}
		gwNew := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec:       ovnv1.VpcNatGatewaySpec{Replicas: 1},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Update,
				Object:    runtime.RawExtension{Raw: gwNewRaw},
				OldObject: runtime.RawExtension{Raw: gwOldRaw},
			},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "replica count reduction from HA (>1) to non-HA (1) is not supported")
	})

	t.Run("Update - non-HA to HA Increase", func(t *testing.T) {
		gwOld := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec:       ovnv1.VpcNatGatewaySpec{Replicas: 1},
		}
		gwNew := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec:       ovnv1.VpcNatGatewaySpec{Replicas: 2},
		}
		gwOldRaw, _ := json.Marshal(gwOld)
		gwNewRaw, _ := json.Marshal(gwNew)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Update,
				Object:    runtime.RawExtension{Raw: gwNewRaw},
				OldObject: runtime.RawExtension{Raw: gwOldRaw},
			},
		}

		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "replica count increase from non-HA (1) to HA (>1) is not supported")
	})

	t.Run("ConfigMap not found", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: gwRaw},
			},
		}

		cache := &mockCache{objects: map[string]runtime.Object{}}

		v := &ValidatingHook{decoder: decoder, cache: cache}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "configMap \"ovn-vpc-nat-config\" not configured")
	})

	t.Run("VpcNatGatewayConfig disabled", func(t *testing.T) {
		gw := ovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: gwRaw},
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"enable-vpc-nat-gw": "false"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "missing-vpc",
				Subnet:   "test-subnet",
				LanIP:    "10.0.0.10",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: gwRaw},
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"enable-vpc-nat-gw": "true"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
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
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: gwRaw},
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{
					ObjectMeta: metav1.ObjectMeta{Name: "test-vpc"},
					Spec:       ovnv1.VpcSpec{EnableBfd: false},
				},
				"/test-subnet": &ovnv1.Subnet{
					ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
					Spec:       ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc:      "test-vpc",
				Subnet:   "test-subnet",
				LanIP:    "192.168.1.1",
				Replicas: 1,
			},
		}
		gwRaw, _ := json.Marshal(gw)

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: gwRaw},
			},
		}

		cache := &mockCache{
			objects: map[string]runtime.Object{
				"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"image": "test-image"},
				},
				"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem},
					Data:       map[string]string{"enable-vpc-nat-gw": "true"},
				},
				"/test-vpc": &ovnv1.Vpc{ObjectMeta: metav1.ObjectMeta{Name: "test-vpc"}},
				"/test-subnet": &ovnv1.Subnet{
					ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
					Spec:       ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
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
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: []byte("invalid-json")},
			},
		}
		v := &ValidatingHook{decoder: decoder}
		resp := v.VpcNatGwCreateOrUpdateHook(context.Background(), req)
		require.False(t, resp.Allowed)
		require.Equal(t, int32(http.StatusBadRequest), resp.Result.Code)
	})
}
