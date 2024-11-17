package anp

import (
	"fmt"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	netpolv1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	"sigs.k8s.io/network-policy-api/conformance/tests"
	netpolv1config "sigs.k8s.io/network-policy-api/conformance/utils/config"
	"sigs.k8s.io/network-policy-api/conformance/utils/suite"
)

const (
	NetworkPolicyAPIRepoURL = "https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/v0.1.5"
	reportFileName          = "anp-test-report.yaml"
)

var baseManifests = fmt.Sprintf("%s/conformance/base/manifests.yaml", NetworkPolicyAPIRepoURL)

func TestAdminNetworkPolicyConformance(t *testing.T) {
	t.Log("Configuring environment for adminnetworkpolicies conformance tests")
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("Error loading Kubernetes config: %v", err)
	}
	client, err := client.New(cfg, client.Options{})
	if err != nil {
		t.Fatalf("Error initializing Kubernetes client: %v", err)
	}
	kubeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		t.Fatalf("error building Kube config for client-go: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		t.Fatalf("error when creating Kubernetes ClientSet: %v", err)
	}
	err = netpolv1alpha1.AddToScheme(client.Scheme())
	if err != nil {
		t.Fatalf("Error initializing API scheme: %v", err)
	}

	t.Log("Starting the admin network policy conformance test suite")
	profiles := sets.Set[suite.ConformanceProfileName]{}
	profiles.Insert(suite.ConformanceProfileName(suite.SupportAdminNetworkPolicy))
	profiles.Insert(suite.ConformanceProfileName(suite.SupportBaselineAdminNetworkPolicy))
	cSuite, err := suite.NewConformanceProfileTestSuite(
		suite.ConformanceProfileOptions{
			Options: suite.Options{
				Client:               client,
				ClientSet:            clientset,
				KubeConfig:           *cfg,
				Debug:                true,
				CleanupBaseResources: true,
				SupportedFeatures:    suite.CoreFeatures,
				BaseManifests:        baseManifests,
				TimeoutConfig:        netpolv1config.TimeoutConfig{GetTimeout: 300 * time.Second},
			},
			ConformanceProfiles: profiles,
		})
	if err != nil {
		t.Fatalf("error creating conformance test suite: %v", err)
	}
	cSuite.Setup(t)
	cSuite.Run(t, tests.ConformanceTests)

	report, err := cSuite.Report()
	if err != nil {
		t.Fatalf("error generating conformance profile report: %v", err)
	}
	t.Logf("Printing report...%v", report)

	rawReport, err := yaml.Marshal(report)
	if err != nil {
		t.Fatalf("error marshalling conformance profile report: %v", err)
	}
	err = os.WriteFile("../../"+reportFileName, rawReport, 0o600)
	if err != nil {
		t.Fatalf("error writing conformance profile report: %v", err)
	}
}
