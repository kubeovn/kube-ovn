package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformers "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Helper function to create a test controller with optional initial objects
func newTestController(subnets []*kubeovnv1.Subnet, configMaps []*corev1.ConfigMap) *Controller {
	// Create clientsets
	var kubeObjects []runtime.Object
	for _, cm := range configMaps {
		kubeObjects = append(kubeObjects, cm)
	}
	kubeClient := fake.NewSimpleClientset(kubeObjects...)

	var kubeovnObjects []runtime.Object
	for _, subnet := range subnets {
		kubeovnObjects = append(kubeovnObjects, subnet)
	}
	kubeOvnClient := kubeovnfake.NewSimpleClientset(kubeovnObjects...)

	// Create informer factories
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	kubeovnInformerFactory := kubeovninformers.NewSharedInformerFactory(kubeOvnClient, 0)

	config := &Configuration{
		KubeClient:              kubeClient,
		KubeOvnClient:           kubeOvnClient,
		ExternalGatewaySwitch:   "external", // Default name
		ExternalGatewayConfigNS: "kube-system",
	}

	controller := &Controller{
		config:           config,
		configMapsLister: kubeInformerFactory.Core().V1().ConfigMaps().Lister(),
		subnetsLister:    kubeovnInformerFactory.Kubeovn().V1().Subnets().Lister(),
	}

	// Start informers and wait for cache sync
	stopCh := make(chan struct{})
	defer close(stopCh)

	kubeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)

	kubeInformerFactory.WaitForCacheSync(stopCh)
	kubeovnInformerFactory.WaitForCacheSync(stopCh)

	// Give informers time to sync
	time.Sleep(100 * time.Millisecond)

	return controller
}

// Test Scenario 1: Default "external" subnet does NOT exist, ConfigMap NOT specified
// Expected: Return default name "external" (will fail later with subnet not found)
func TestGetExternalGatewaySwitch_Scenario1_DefaultNotExist_ConfigMapNotSpecified(t *testing.T) {
	c := newTestController(nil, nil)

	result, err := c.getExternalGatewaySwitch()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	expected := "external"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// Test Scenario 2: Default "external" subnet EXISTS, ConfigMap NOT specified
// Expected: Use default "external"
func TestGetExternalGatewaySwitch_Scenario2_DefaultExists_ConfigMapNotSpecified(t *testing.T) {
	defaultSubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "external",
		},
	}

	c := newTestController([]*kubeovnv1.Subnet{defaultSubnet}, nil)

	result, err := c.getExternalGatewaySwitch()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	expected := "external"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// Test Scenario 3: Default "external" subnet does NOT exist, ConfigMap specifies "custom-ext"
// Expected: Use "custom-ext"
func TestGetExternalGatewaySwitch_Scenario3_DefaultNotExist_ConfigMapSpecifiedDifferent(t *testing.T) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ExternalGatewayConfig,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"enable-external-gw": "true",
			"external-gw-switch": "custom-ext",
		},
	}

	c := newTestController(nil, []*corev1.ConfigMap{configMap})

	result, err := c.getExternalGatewaySwitch()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	expected := "custom-ext"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// Test Scenario 4: Default "external" subnet EXISTS, ConfigMap specifies "custom-ext" (different)
// Expected: ERROR - configuration conflict
func TestGetExternalGatewaySwitch_Scenario4_DefaultExists_ConfigMapSpecifiedDifferent(t *testing.T) {
	defaultSubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "external",
		},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ExternalGatewayConfig,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"enable-external-gw": "true",
			"external-gw-switch": "custom-ext",
		},
	}

	c := newTestController([]*kubeovnv1.Subnet{defaultSubnet}, []*corev1.ConfigMap{configMap})

	_, err := c.getExternalGatewaySwitch()
	if err == nil {
		t.Error("expected error due to configuration conflict, but got nil")
		return
	}

	expectedErrMsg := "configuration conflict"
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("expected error message to contain '%s', got: %v", expectedErrMsg, err)
	}
}

// Test Scenario 5: Default "external" subnet EXISTS, ConfigMap specifies "external" (same name)
// Expected: Use default "external" (no conflict)
func TestGetExternalGatewaySwitch_Scenario5_DefaultExists_ConfigMapSpecifiedSame(t *testing.T) {
	defaultSubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "external",
		},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ExternalGatewayConfig,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"enable-external-gw": "true",
			"external-gw-switch": "external",
		},
	}

	c := newTestController([]*kubeovnv1.Subnet{defaultSubnet}, []*corev1.ConfigMap{configMap})

	result, err := c.getExternalGatewaySwitch()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	expected := "external"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// Test ConfigMap disabled: enable-external-gw = false
// Expected: Return default regardless of ConfigMap content
func TestGetExternalGatewaySwitch_ConfigMapDisabled(t *testing.T) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ExternalGatewayConfig,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"enable-external-gw": "false",
			"external-gw-switch": "custom-ext", // Should be ignored
		},
	}

	c := newTestController(nil, []*corev1.ConfigMap{configMap})

	result, err := c.getExternalGatewaySwitch()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	expected := "external"
	if result != expected {
		t.Errorf("expected %s (should ignore ConfigMap when disabled), got %s", expected, result)
	}
}

// Test getExternalGatewaySwitchWithConfigMap directly
func TestGetExternalGatewaySwitchWithConfigMap_AllScenarios(t *testing.T) {
	tests := []struct {
		name             string
		defaultExists    bool
		configSwitch     string
		expectedResult   string
		expectError      bool
		errorMsgContains string
	}{
		{
			name:           "Scenario 1: default not exist, config not specified",
			defaultExists:  false,
			configSwitch:   "",
			expectedResult: "external",
			expectError:    false,
		},
		{
			name:           "Scenario 2: default exists, config not specified",
			defaultExists:  true,
			configSwitch:   "",
			expectedResult: "external",
			expectError:    false,
		},
		{
			name:           "Scenario 3: default not exist, config specified",
			defaultExists:  false,
			configSwitch:   "custom-ext",
			expectedResult: "custom-ext",
			expectError:    false,
		},
		{
			name:             "Scenario 4: default exists, config specified different",
			defaultExists:    true,
			configSwitch:     "custom-ext",
			expectedResult:   "",
			expectError:      true,
			errorMsgContains: "configuration conflict",
		},
		{
			name:           "Scenario 5: default exists, config specified same",
			defaultExists:  true,
			configSwitch:   "external",
			expectedResult: "external",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var subnets []*kubeovnv1.Subnet
			if tt.defaultExists {
				subnets = []*kubeovnv1.Subnet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "external",
						},
					},
				}
			}

			c := newTestController(subnets, nil)

			configData := map[string]string{}
			if tt.configSwitch != "" {
				configData["external-gw-switch"] = tt.configSwitch
			}

			result, err := c.getExternalGatewaySwitchWithConfigMap(configData)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errorMsgContains != "" && !strings.Contains(err.Error(), tt.errorMsgContains) {
					t.Errorf("expected error containing '%s', got: %v", tt.errorMsgContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				if result != tt.expectedResult {
					t.Errorf("expected result %s, got %s", tt.expectedResult, result)
				}
			}
		})
	}
}

// Test with actual Kubernetes client operations
func TestGetExternalGatewaySwitch_WithClientOperations(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	kubeOvnClient := kubeovnfake.NewSimpleClientset()

	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	kubeovnInformerFactory := kubeovninformers.NewSharedInformerFactory(kubeOvnClient, 0)

	config := &Configuration{
		KubeClient:              kubeClient,
		KubeOvnClient:           kubeOvnClient,
		ExternalGatewaySwitch:   "external",
		ExternalGatewayConfigNS: "kube-system",
	}

	controller := &Controller{
		config:           config,
		configMapsLister: kubeInformerFactory.Core().V1().ConfigMaps().Lister(),
		subnetsLister:    kubeovnInformerFactory.Kubeovn().V1().Subnets().Lister(),
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	kubeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)

	// Create subnet after informer starts
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "external",
		},
	}
	_, err := kubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), subnet, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create subnet: %v", err)
	}

	// Create ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ExternalGatewayConfig,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"enable-external-gw": "true",
			"external-gw-switch": "custom-ext",
		},
	}
	_, err = kubeClient.CoreV1().ConfigMaps("kube-system").Create(context.Background(), configMap, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}

	// Wait for informer cache sync
	time.Sleep(200 * time.Millisecond)

	// This should return error due to conflict
	_, err = controller.getExternalGatewaySwitch()
	if err == nil {
		t.Error("expected configuration conflict error, got nil")
	} else if !strings.Contains(err.Error(), "configuration conflict") {
		t.Errorf("expected configuration conflict error, got: %v", err)
	}
}
