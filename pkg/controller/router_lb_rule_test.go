package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ---------------------------------------------------------------------------
// Pure function tests
// ---------------------------------------------------------------------------

func Test_generateRlrHeadlessService(t *testing.T) {
	makeRlr := func(name, vpc, eip, ns string, selectors []string, ports []kubeovnv1.RouterLBRulePort) *kubeovnv1.RouterLBRule {
		return &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: kubeovnv1.RouterLBRuleSpec{
				Vpc:      vpc,
				OvnEip:   eip,
				Namespace: ns,
				Selector: selectors,
				Ports:    ports,
			},
		}
	}
	port80 := kubeovnv1.RouterLBRulePort{Name: "http", Port: 80, TargetPort: 8080, Protocol: "TCP"}

	tests := []struct {
		name        string
		rlr         *kubeovnv1.RouterLBRule
		oldSvc      *corev1.Service
		svcName     string
		namespace   string
		vip         string
		wantVipAnno string
		wantRouter  string
		wantFamilies []corev1.IPFamily
		wantPolicy  corev1.IPFamilyPolicy
		wantClusterIP string
	}{
		{
			name:          "IPv4 only",
			rlr:           makeRlr("rlr1", "vpc1", "eip1", "", nil, []kubeovnv1.RouterLBRulePort{port80}),
			svcName:       "rlr-rlr1",
			namespace:     "default",
			vip:           "10.0.0.1",
			wantVipAnno:   "10.0.0.1",
			wantRouter:    "vpc1",
			wantFamilies:  []corev1.IPFamily{corev1.IPv4Protocol},
			wantPolicy:    corev1.IPFamilyPolicySingleStack,
			wantClusterIP: corev1.ClusterIPNone,
		},
		{
			name:          "IPv6 only",
			rlr:           makeRlr("rlr2", "vpc1", "eip1", "", nil, []kubeovnv1.RouterLBRulePort{port80}),
			svcName:       "rlr-rlr2",
			namespace:     "default",
			vip:           "fd00::1",
			wantVipAnno:   "fd00::1",
			wantRouter:    "vpc1",
			wantFamilies:  []corev1.IPFamily{corev1.IPv6Protocol},
			wantPolicy:    corev1.IPFamilyPolicySingleStack,
			wantClusterIP: corev1.ClusterIPNone,
		},
		{
			name:          "dual-stack",
			rlr:           makeRlr("rlr3", "vpc1", "eip1", "", nil, []kubeovnv1.RouterLBRulePort{port80}),
			svcName:       "rlr-rlr3",
			namespace:     "default",
			vip:           "10.0.0.1,fd00::1",
			wantVipAnno:   "10.0.0.1,fd00::1",
			wantRouter:    "vpc1",
			wantFamilies:  []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			wantPolicy:    corev1.IPFamilyPolicyPreferDualStack,
			wantClusterIP: corev1.ClusterIPNone,
		},
		{
			name: "selector parsed from colon-separated strings",
			rlr: makeRlr("rlr4", "vpc1", "eip1", "", []string{"app: foo", "env: prod"}, []kubeovnv1.RouterLBRulePort{port80}),
			svcName:   "rlr-rlr4",
			namespace: "default",
			vip:       "10.0.0.2",
		},
		{
			name: "update existing service preserves extra annotations",
			rlr:  makeRlr("rlr5", "vpc1", "eip1", "", nil, []kubeovnv1.RouterLBRulePort{port80}),
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rlr-rlr5",
					Namespace: "default",
					Annotations: map[string]string{
						"custom-anno": "keep-me",
					},
				},
			},
			svcName:     "rlr-rlr5",
			namespace:   "default",
			vip:         "10.0.0.3",
			wantVipAnno: "10.0.0.3",
			wantRouter:  "vpc1",
		},
		{
			name: "health-check annotation propagated",
			rlr: func() *kubeovnv1.RouterLBRule {
				r := makeRlr("rlr6", "vpc1", "eip1", "", nil, []kubeovnv1.RouterLBRulePort{port80})
				r.Annotations = map[string]string{
					util.ServiceHealthCheck: "true",
				}
				return r
			}(),
			svcName:   "rlr-rlr6",
			namespace: "default",
			vip:       "10.0.0.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := generateRlrHeadlessService(tt.rlr, tt.oldSvc, tt.svcName, tt.namespace, tt.vip)

			require.NotNil(t, svc)
			assert.Equal(t, tt.svcName, svc.Name)
			assert.Equal(t, tt.namespace, svc.Namespace)

			if tt.wantVipAnno != "" {
				assert.Equal(t, tt.wantVipAnno, svc.Annotations[util.RouterLBRuleVipsAnnotation])
			}
			if tt.wantRouter != "" {
				assert.Equal(t, tt.wantRouter, svc.Annotations[util.LogicalRouterAnnotation])
			}
			if len(tt.wantFamilies) > 0 {
				assert.Equal(t, tt.wantFamilies, svc.Spec.IPFamilies)
				require.NotNil(t, svc.Spec.IPFamilyPolicy)
				assert.Equal(t, tt.wantPolicy, *svc.Spec.IPFamilyPolicy)
			}
			if tt.wantClusterIP != "" {
				assert.Equal(t, tt.wantClusterIP, svc.Spec.ClusterIP)
			}

			switch tt.name {
			case "selector parsed from colon-separated strings":
				assert.Equal(t, map[string]string{"app": "foo", "env": "prod"}, svc.Spec.Selector)
			case "update existing service preserves extra annotations":
				assert.Equal(t, "keep-me", svc.Annotations["custom-anno"])
			case "health-check annotation propagated":
				assert.Equal(t, "true", svc.Annotations[util.ServiceHealthCheck])
			}
		})
	}
}

func Test_generateRlrEndpoints(t *testing.T) {
	ports := []kubeovnv1.RouterLBRulePort{
		{Name: "http", Port: 80, TargetPort: 8080, Protocol: "TCP"},
	}

	t.Run("TargetRef.Namespace uses passed namespace not rlr.Namespace", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			// Namespace is empty because RouterLBRule is cluster-scoped
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec: kubeovnv1.RouterLBRuleSpec{
				Endpoints: []string{"192.168.1.10", "192.168.1.11"},
				Ports:     ports,
			},
		}
		eps := generateRlrEndpoints(rlr, nil, "rlr-rlr1", "custom-ns")

		require.Len(t, eps.Subsets, 1)
		require.Len(t, eps.Subsets[0].Addresses, 2)
		for _, addr := range eps.Subsets[0].Addresses {
			require.NotNil(t, addr.TargetRef)
			assert.Equal(t, "custom-ns", addr.TargetRef.Namespace,
				"TargetRef.Namespace must be the resolved service namespace, not the empty cluster-scoped RLR namespace")
		}
	})

	t.Run("endpoint port uses TargetPort not Port", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec: kubeovnv1.RouterLBRuleSpec{
				Endpoints: []string{"10.0.0.1"},
				Ports:     ports,
			},
		}
		eps := generateRlrEndpoints(rlr, nil, "rlr-rlr1", "default")

		require.Len(t, eps.Subsets[0].Ports, 1)
		assert.Equal(t, int32(8080), eps.Subsets[0].Ports[0].Port,
			"EndpointPort should use TargetPort (8080), not Port (80)")
	})

	t.Run("update reuses existing endpoint metadata", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec: kubeovnv1.RouterLBRuleSpec{
				Endpoints: []string{"10.0.0.2"},
				Ports:     ports,
			},
		}
		oldEps := &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "rlr-rlr1",
				Namespace:       "default",
				ResourceVersion: "42",
				Labels:          map[string]string{"existing": "label"},
			},
		}
		eps := generateRlrEndpoints(rlr, oldEps, "rlr-rlr1", "default")

		assert.Equal(t, "42", eps.ResourceVersion, "ResourceVersion must be preserved from oldEps")
		assert.Equal(t, "label", eps.Labels["existing"])
		assert.Equal(t, "10.0.0.2", eps.Subsets[0].Addresses[0].IP)
	})

	t.Run("no static endpoints produces empty addresses", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec:       kubeovnv1.RouterLBRuleSpec{Ports: ports},
		}
		eps := generateRlrEndpoints(rlr, nil, "rlr-rlr1", "default")

		require.Len(t, eps.Subsets, 1)
		assert.Empty(t, eps.Subsets[0].Addresses)
	})
}

func Test_newRlrInfo(t *testing.T) {
	t.Run("empty namespace defaults to 'default'", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec: kubeovnv1.RouterLBRuleSpec{
				Namespace: "",
				OvnEip:    "eip1",
				Vpc:       "vpc1",
				Ports:     []kubeovnv1.RouterLBRulePort{{Port: 80}},
			},
		}
		info := newRlrInfo(rlr)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
	})

	t.Run("explicit namespace is preserved", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec:       kubeovnv1.RouterLBRuleSpec{Namespace: "custom-ns"},
		}
		info := newRlrInfo(rlr)
		assert.Equal(t, "custom-ns", info.Namespace)
	})

	t.Run("ports extracted from spec", func(t *testing.T) {
		rlr := &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rlr1"},
			Spec: kubeovnv1.RouterLBRuleSpec{
				Ports: []kubeovnv1.RouterLBRulePort{{Port: 80}, {Port: 443}},
			},
		}
		info := newRlrInfo(rlr)
		assert.Equal(t, []int32{80, 443}, info.Ports)
	})
}

// getVipIps is in service.go; these cases cover the RouterLBRuleVipsAnnotation
// branch added alongside the fix.
func Test_getVipIps_routerLBRule(t *testing.T) {
	tests := []struct {
		name     string
		svc      *corev1.Service
		wantIPs  []string
		wantEmpty bool
	}{
		{
			name: "RouterLBRuleVipsAnnotation single IPv4",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.RouterLBRuleVipsAnnotation: "10.0.0.1",
					},
				},
				Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone},
			},
			wantIPs: []string{"10.0.0.1"},
		},
		{
			name: "RouterLBRuleVipsAnnotation dual-stack splits correctly",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.RouterLBRuleVipsAnnotation: "10.0.0.1,fd00::1",
					},
				},
				Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone},
			},
			wantIPs: []string{"10.0.0.1", "fd00::1"},
		},
		{
			name: "headless service without annotation returns empty",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone},
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips := getVipIps(tt.svc)
			if tt.wantEmpty {
				assert.Empty(t, ips)
			} else {
				assert.Equal(t, tt.wantIPs, ips)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Controller tests — checkEipPortConflict
// ---------------------------------------------------------------------------

func Test_checkEipPortConflict(t *testing.T) {
	existingRlr := &kubeovnv1.RouterLBRule{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-rlr"},
		Spec: kubeovnv1.RouterLBRuleSpec{
			OvnEip: "eip1",
			Ports:  []kubeovnv1.RouterLBRulePort{{Port: 80}},
		},
	}
	existingDnat := &kubeovnv1.OvnDnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "existing-dnat",
			Labels: map[string]string{util.VpcDnatEPortLabel: "443"},
		},
		Spec: kubeovnv1.OvnDnatRuleSpec{OvnEip: "eip1"},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		RouterLBRules: []*kubeovnv1.RouterLBRule{existingRlr},
		OvnDnatRules:  []*kubeovnv1.OvnDnatRule{existingDnat},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	tests := []struct {
		name        string
		eip         string
		port        string
		excludeRlr  string
		excludeDnat string
		wantErr     bool
	}{
		{
			name:    "no conflict",
			eip:     "eip2",
			port:    "80",
			wantErr: false,
		},
		{
			name:    "conflict with RouterLBRule on same EIP+port",
			eip:     "eip1",
			port:    "80",
			wantErr: true,
		},
		{
			name:       "self-exclusion skips own RLR",
			eip:        "eip1",
			port:       "80",
			excludeRlr: "existing-rlr",
			wantErr:    false,
		},
		{
			name:    "conflict with OvnDnatRule on same EIP+port",
			eip:     "eip1",
			port:    "443",
			wantErr: true,
		},
		{
			name:        "DNAT exclusion skips own DNAT",
			eip:         "eip1",
			port:        "443",
			excludeDnat: "existing-dnat",
			wantErr:     false,
		},
		{
			name:    "different port no conflict",
			eip:     "eip1",
			port:    "8080",
			wantErr: false,
		},
		{
			name:    "different EIP no conflict",
			eip:     "eip99",
			port:    "80",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ctrl.checkEipPortConflict(tt.eip, tt.port, tt.excludeRlr, tt.excludeDnat)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Controller tests — handleAddOrUpdateRouterLBRule
// ---------------------------------------------------------------------------

func Test_handleAddOrUpdateRouterLBRule(t *testing.T) {
	makeEip := func(name, v4ip, specType string) *kubeovnv1.OvnEip {
		return &kubeovnv1.OvnEip{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       kubeovnv1.OvnEipSpec{Type: specType},
			Status:     kubeovnv1.OvnEipStatus{V4Ip: v4ip},
		}
	}
	makeVpc := func(name, tcpLB string) *kubeovnv1.Vpc {
		return &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status:     kubeovnv1.VpcStatus{TCPLoadBalancer: tcpLB},
		}
	}
	makeRlr := func(name, eip, vpc string, ports []kubeovnv1.RouterLBRulePort) *kubeovnv1.RouterLBRule {
		return &kubeovnv1.RouterLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: kubeovnv1.RouterLBRuleSpec{
				OvnEip: eip,
				Vpc:    vpc,
				Ports:  ports,
			},
		}
	}

	t.Run("RLR not found returns nil", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		assert.NoError(t, fc.fakeController.handleAddOrUpdateRouterLBRule("nonexistent"))
	})

	t.Run("empty OvnEip is skipped without error", func(t *testing.T) {
		rlr := makeRlr("rlr1", "", "vpc1", nil)
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
		})
		require.NoError(t, err)
		assert.NoError(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("empty Vpc returns error", func(t *testing.T) {
		rlr := makeRlr("rlr1", "eip1", "", []kubeovnv1.RouterLBRulePort{{Port: 80}})
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
			OvnEips:       []*kubeovnv1.OvnEip{makeEip("eip1", "10.0.0.1", util.OvnEipTypeNAT)},
		})
		require.NoError(t, err)
		assert.Error(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("no ports returns error", func(t *testing.T) {
		rlr := makeRlr("rlr1", "eip1", "vpc1", nil)
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
			OvnEips:       []*kubeovnv1.OvnEip{makeEip("eip1", "10.0.0.1", util.OvnEipTypeNAT)},
		})
		require.NoError(t, err)
		assert.Error(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("LSP-type EIP returns error", func(t *testing.T) {
		rlr := makeRlr("rlr1", "lsp-eip", "vpc1", []kubeovnv1.RouterLBRulePort{{Port: 80}})
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
			// V4Ip must be non-empty to pass GetOvnEip readiness check, but Spec.Type is LSP
			OvnEips: []*kubeovnv1.OvnEip{makeEip("lsp-eip", "10.0.0.1", util.OvnEipTypeLSP)},
		})
		require.NoError(t, err)
		assert.Error(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("EIP with no IP returns error", func(t *testing.T) {
		rlr := makeRlr("rlr1", "empty-eip", "vpc1", []kubeovnv1.RouterLBRulePort{{Port: 80}})
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
			OvnEips:       []*kubeovnv1.OvnEip{makeEip("empty-eip", "", util.OvnEipTypeNAT)},
		})
		require.NoError(t, err)
		assert.Error(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("port conflict with another RouterLBRule returns error", func(t *testing.T) {
		// "rlr1" claims eip1:80; "existing-rlr" already owns eip1:80.
		existing := makeRlr("existing-rlr", "eip1", "vpc1", []kubeovnv1.RouterLBRulePort{{Port: 80}})
		rlr := makeRlr("rlr1", "eip1", "vpc1", []kubeovnv1.RouterLBRulePort{{Port: 80}})
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{existing, rlr},
			OvnEips:       []*kubeovnv1.OvnEip{makeEip("eip1", "10.0.0.1", util.OvnEipTypeNAT)},
			Vpcs:          []*kubeovnv1.Vpc{makeVpc("vpc1", "")},
		})
		require.NoError(t, err)
		assert.Error(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("port conflict with OvnDnatRule returns error", func(t *testing.T) {
		// "rlr1" claims eip1:443; an OvnDnatRule already uses eip1:443.
		dnat := &kubeovnv1.OvnDnatRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "existing-dnat",
				Labels: map[string]string{util.VpcDnatEPortLabel: "443"},
			},
			Spec: kubeovnv1.OvnDnatRuleSpec{OvnEip: "eip1"},
		}
		rlr := makeRlr("rlr1", "eip1", "vpc1", []kubeovnv1.RouterLBRulePort{{Port: 443}})
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
			OvnEips:       []*kubeovnv1.OvnEip{makeEip("eip1", "10.0.0.1", util.OvnEipTypeNAT)},
			Vpcs:          []*kubeovnv1.Vpc{makeVpc("vpc1", "")},
			OvnDnatRules:  []*kubeovnv1.OvnDnatRule{dnat},
		})
		require.NoError(t, err)
		assert.Error(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))
	})

	t.Run("happy path creates service and attaches LBs", func(t *testing.T) {
		rlr := makeRlr("rlr1", "eip1", "vpc1", []kubeovnv1.RouterLBRulePort{
			{Name: "http", Port: 80, TargetPort: 8080, Protocol: "TCP"},
		})
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			RouterLBRules: []*kubeovnv1.RouterLBRule{rlr},
			OvnEips:       []*kubeovnv1.OvnEip{makeEip("eip1", "192.168.1.100", util.OvnEipTypeNAT)},
			Vpcs:          []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LogicalRouterUpdateLoadBalancers(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		require.NoError(t, fc.fakeController.handleAddOrUpdateRouterLBRule("rlr1"))

		// Service must exist with the correct VIP and router annotations.
		svc, err := fc.fakeController.config.KubeClient.CoreV1().
			Services(metav1.NamespaceDefault).
			Get(context.Background(), "rlr-rlr1", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "192.168.1.100", svc.Annotations[util.RouterLBRuleVipsAnnotation])
		assert.Equal(t, "vpc1", svc.Annotations[util.LogicalRouterAnnotation])
		assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)

		// Status must be updated with service reference.
		updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().
			RouterLBRules().
			Get(context.Background(), "rlr1", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "default/rlr-rlr1", updated.Status.Service)
		assert.Contains(t, updated.Status.Ports, "80/TCP")
	})
}

// ---------------------------------------------------------------------------
// Controller tests — handleDelRouterLBRule
// ---------------------------------------------------------------------------

func Test_handleDelRouterLBRule(t *testing.T) {
	const (
		testVpc    = "test-vpc"
		testTCPLB  = "test-tcp-lb"
		testEIP    = "192.168.1.100"
		testPort   = int32(80)
		testVip    = "192.168.1.100:80"
		svcName    = "rlr-test-rlr"
		svcNS      = metav1.NamespaceDefault
	)

	makeSvc := func(withVipAnno bool) *corev1.Service {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNS,
				Annotations: map[string]string{
					util.LogicalRouterAnnotation: testVpc,
				},
			},
		}
		if withVipAnno {
			svc.Annotations[util.RouterLBRuleVipsAnnotation] = testEIP
		}
		return svc
	}
	makeInfo := func() *RlrInfo {
		return &RlrInfo{Name: "test-rlr", Namespace: svcNS, Ports: []int32{testPort}}
	}

	t.Run("service not found exits cleanly without OVN calls", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		// No mock expectations — any unexpected OVN call fails the test.
		assert.NoError(t, fc.fakeController.handleDelRouterLBRule(makeInfo()))
	})

	t.Run("service without VIP annotation only deletes the service", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Services: []*corev1.Service{makeSvc(false)},
		})
		require.NoError(t, err)
		// No mock expectations — no OVN calls expected.
		assert.NoError(t, fc.fakeController.handleDelRouterLBRule(makeInfo()))

		_, err = fc.fakeController.config.KubeClient.CoreV1().
			Services(svcNS).Get(context.Background(), svcName, metav1.GetOptions{})
		assert.True(t, k8serrors.IsNotFound(err), "service must be deleted")
	})

	t.Run("happy path deletes VIP from LB and cleans up LBHCs", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Services: []*corev1.Service{makeSvc(true)},
			Vpcs: []*kubeovnv1.Vpc{{
				ObjectMeta: metav1.ObjectMeta{Name: testVpc},
				Status:     kubeovnv1.VpcStatus{TCPLoadBalancer: testTCPLB},
			}},
		})
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LoadBalancerDeleteVip(testTCPLB, testVip, true).
			Return(nil)
		fc.mockOvnClient.EXPECT().
			ListLoadBalancerHealthChecks(gomock.Any()).
			Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		require.NoError(t, fc.fakeController.handleDelRouterLBRule(makeInfo()))

		_, err = fc.fakeController.config.KubeClient.CoreV1().
			Services(svcNS).Get(context.Background(), svcName, metav1.GetOptions{})
		assert.True(t, k8serrors.IsNotFound(err), "service must be deleted")
	})

	t.Run("VPC not found skips VIP deletion but still cleans LBHCs", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Services: []*corev1.Service{makeSvc(true)},
			// VPC intentionally absent.
		})
		require.NoError(t, err)

		// LoadBalancerDeleteVip must NOT be called (vpcLBNames is nil).
		fc.mockOvnClient.EXPECT().
			ListLoadBalancerHealthChecks(gomock.Any()).
			Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		assert.NoError(t, fc.fakeController.handleDelRouterLBRule(makeInfo()))
	})
}

// ---------------------------------------------------------------------------
// Enqueue logic — IsRecreate flag
// ---------------------------------------------------------------------------

func Test_enqueueUpdateRouterLBRule_isRecreate(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.updateRouterLBRuleQueue = newTypedRateLimitingQueue[*RlrInfo]("UpdateRouterLBRuleTest", nil)

	base := &kubeovnv1.RouterLBRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rlr1", ResourceVersion: "1"},
		Spec: kubeovnv1.RouterLBRuleSpec{
			OvnEip:    "eip1",
			Vpc:       "vpc1",
			Namespace: "default",
			Selector:  []string{"app: foo"},
		},
	}

	drainQueue := func() *RlrInfo {
		item, _ := ctrl.updateRouterLBRuleQueue.Get()
		ctrl.updateRouterLBRuleQueue.Done(item)
		return item
	}

	tests := []struct {
		name           string
		mutate         func(*kubeovnv1.RouterLBRule) *kubeovnv1.RouterLBRule
		wantIsRecreate bool
	}{
		{
			name: "same ResourceVersion is a no-op",
			mutate: func(r *kubeovnv1.RouterLBRule) *kubeovnv1.RouterLBRule {
				return r.DeepCopy() // ResourceVersion unchanged
			},
		},
		{
			name: "OvnEip change triggers recreate",
			mutate: func(r *kubeovnv1.RouterLBRule) *kubeovnv1.RouterLBRule {
				n := r.DeepCopy()
				n.ResourceVersion = "2"
				n.Spec.OvnEip = "eip2"
				return n
			},
			wantIsRecreate: true,
		},
		{
			name: "Vpc change triggers recreate",
			mutate: func(r *kubeovnv1.RouterLBRule) *kubeovnv1.RouterLBRule {
				n := r.DeepCopy()
				n.ResourceVersion = "2"
				n.Spec.Vpc = "vpc2"
				return n
			},
			wantIsRecreate: true,
		},
		{
			name: "Namespace change triggers recreate",
			mutate: func(r *kubeovnv1.RouterLBRule) *kubeovnv1.RouterLBRule {
				n := r.DeepCopy()
				n.ResourceVersion = "2"
				n.Spec.Namespace = "other-ns"
				return n
			},
			wantIsRecreate: true,
		},
		{
			name: "selector-only change does not trigger recreate",
			mutate: func(r *kubeovnv1.RouterLBRule) *kubeovnv1.RouterLBRule {
				n := r.DeepCopy()
				n.ResourceVersion = "2"
				n.Spec.Selector = []string{"app: bar"}
				return n
			},
			wantIsRecreate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newRlr := tt.mutate(base)
			ctrl.enqueueUpdateRouterLBRule(base, newRlr)

			if newRlr.ResourceVersion == base.ResourceVersion {
				assert.Equal(t, 0, ctrl.updateRouterLBRuleQueue.Len(), "no-op must not enqueue")
				return
			}
			require.Equal(t, 1, ctrl.updateRouterLBRuleQueue.Len())
			info := drainQueue()
			assert.Equal(t, tt.wantIsRecreate, info.IsRecreate)
		})
	}
}


