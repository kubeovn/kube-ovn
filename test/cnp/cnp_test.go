package cnp

import (
	"os"
	"path"
	"slices"
	"testing"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	netpolv1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	"sigs.k8s.io/network-policy-api/conformance/tests"
	netpolv1config "sigs.k8s.io/network-policy-api/conformance/utils/config"
	"sigs.k8s.io/network-policy-api/conformance/utils/suite"
)

const (
	NetworkPolicyAPIRepoRaw  = "https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api"
	NetworkPolicyAPIRepoPath = "conformance/base/manifests.yaml"
	cnpReportFileName        = "cnp-test-report.yaml"
)

func TestClusterNetworkPolicyConformance(t *testing.T) {
	content, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("Failed to read go.mod: %v", err)
	}

	mf, err := modfile.Parse("go.mod", content, nil)
	if err != nil {
		t.Fatalf("Failed to parse go.mod: %v", err)
	}

	var version string
	for r := range slices.Values(mf.Require) {
		if r.Mod.Path != "sigs.k8s.io/network-policy-api" {
			continue
		}
		t.Logf("network-policy-api module version: %s", r.Mod.Version)
		version = r.Mod.Version
	}

	gitRef := version
	if module.IsPseudoVersion(version) {
		t.Logf("Pseudo version detected: %s", version)
		if gitRef, err = module.PseudoVersionRev(version); err != nil {
			t.Fatalf("Failed to get git revision from pseudo version: %v", err)
		}
	}
	manifestsURL := path.Join(NetworkPolicyAPIRepoRaw, gitRef, NetworkPolicyAPIRepoPath)
	t.Logf("Using manifests URL: %s", manifestsURL)

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
	err = netpolv1alpha2.Install(client.Scheme())
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
				BaseManifests:        manifestsURL,
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
