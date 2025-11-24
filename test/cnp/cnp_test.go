package anp

import (
	"fmt"
	"os"
	netpolv1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/network-policy-api/conformance/tests"
	netpolv1config "sigs.k8s.io/network-policy-api/conformance/utils/config"
	"sigs.k8s.io/network-policy-api/conformance/utils/suite"
)

const (
	NetworkPolicyCNPAPIRepoURL = "https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/main"
	cnpReportFileName          = "cnp-test-report.yaml"
)

var baseCnpManifests = fmt.Sprintf("%s/conformance/base/manifests.yaml", NetworkPolicyCNPAPIRepoURL)

func TestClusterNetworkPolicyConformance(t *testing.T) {
	t.Log("Configuring environment for clusternetworkpolicies conformance tests")
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
	err = netpolv1alpha2.AddToScheme(client.Scheme())
	if err != nil {
		t.Fatalf("Error initializing API scheme: %v", err)
	}

	t.Log("Starting the admin network policy conformance test suite")
	profiles := sets.Set[suite.ConformanceProfileName]{}
	profiles.Insert(suite.CNPConformanceProfileName)
	cSuite, err := suite.NewConformanceProfileTestSuite(
		suite.ConformanceProfileOptions{
			Options: suite.Options{
				Client:               client,
				ClientSet:            clientset,
				KubeConfig:           *cfg,
				Debug:                true,
				CleanupBaseResources: true,
				SupportedFeatures:    suite.StandardFeatures,
				BaseManifests:        baseCnpManifests,
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
	err = os.WriteFile("../../"+cnpReportFileName, rawReport, 0o600)
	if err != nil {
		t.Fatalf("error writing conformance profile report: %v", err)
	}
}
