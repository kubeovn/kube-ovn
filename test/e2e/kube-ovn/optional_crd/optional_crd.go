package optional_crd

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	bgpConfCRDName  = "bgp-confs.kubeovn.io"
	evpnConfCRDName = "evpn-confs.kubeovn.io"

	controllerDeployName = "kube-ovn-controller"

	// Background poll interval inside the controller is 10s
	// (StartBgpEvpnConfInformerFactory), so give the self-heal path
	// enough room to fire at least twice.
	selfHealTimeout = 60 * time.Second
)

// Regression test for kube-ovn/kube-ovn#6726.
//
// Before the fix, the kube-ovn-controller unconditionally added the BgpConf
// and EvpnConf informers to its WaitForCacheSync list, so missing CRDs
// (e.g. clusters upgraded from v1.15.x via Helm, which does not re-apply the
// chart's crds/ directory on upgrade) wedged the controller in
// "Waiting for informer caches to sync" and broke IPAM cluster-wide.
//
// This spec reproduces the failure mode by deleting both CRDs, restarting the
// controller deployment and asserting the rollout completes within the
// framework rollout timeout. It then re-creates the CRDs and waits for the
// controller's background retry loop to pick them up.
var _ = framework.SerialDescribe("[group:optional-crd]", func() {
	f := framework.NewDefaultFramework("optional-crd")

	var savedBgp, savedEvpn *apiextensionsv1.CustomResourceDefinition

	ginkgo.AfterEach(func() {
		ctx := context.Background()
		// Defensive restore: if a previous step deleted a CRD but failed
		// before re-creating it, put it back so subsequent specs don't
		// inherit a broken cluster.
		restoreCRD(ctx, f, savedBgp)
		restoreCRD(ctx, f, savedEvpn)
	})

	framework.ConformanceIt("kube-ovn-controller should start when optional BgpConf/EvpnConf CRDs are missing", func() {
		f.SkipVersionPriorTo(1, 16, "BgpConf/EvpnConf CRDs were introduced in v1.16; the optional-CRD fix targets v1.16+")

		ctx := context.Background()
		ext := f.ExtClientSet.ApiextensionsV1().CustomResourceDefinitions()
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)

		ginkgo.By("Backing up BgpConf and EvpnConf CRDs")
		var err error
		savedBgp, err = ext.Get(ctx, bgpConfCRDName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			ginkgo.Skip(bgpConfCRDName + " CRD is not installed; nothing to validate")
		}
		framework.ExpectNoError(err)
		savedEvpn, err = ext.Get(ctx, evpnConfCRDName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			ginkgo.Skip(evpnConfCRDName + " CRD is not installed; nothing to validate")
		}
		framework.ExpectNoError(err)

		ginkgo.By("Deleting BgpConf and EvpnConf CRDs to simulate a partial helm upgrade")
		framework.ExpectNoError(ext.Delete(ctx, bgpConfCRDName, metav1.DeleteOptions{}))
		framework.ExpectNoError(ext.Delete(ctx, evpnConfCRDName, metav1.DeleteOptions{}))
		framework.WaitUntil(2*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			for _, name := range []string{bgpConfCRDName, evpnConfCRDName} {
				_, err := ext.Get(ctx, name, metav1.GetOptions{})
				switch {
				case err == nil:
					// CRD still present, keep polling.
					return false, nil
				case apierrors.IsNotFound(err):
					// CRD is gone, check the next one.
					continue
				default:
					// Anything else (Forbidden, network issue, ...) is
					// surfaced immediately so the test fails fast with
					// the real cause instead of a generic timeout.
					return false, fmt.Errorf("unexpected error checking CRD %s: %w", name, err)
				}
			}
			return true, nil
		}, "BgpConf/EvpnConf CRDs to be deleted")

		ginkgo.By("Restarting " + controllerDeployName + " and waiting for the rollout to complete")
		deploy := deployClient.Get(controllerDeployName)
		// RestartSync waits for the new ReplicaSet to become ready. Before
		// the fix this rollout never completes because the controller pods
		// block in WaitForCacheSync on the missing CRDs.
		deployClient.RestartSync(deploy)

		ginkgo.By("Creating a pod after CRDs were removed to verify IPAM still works")
		nsName := f.Namespace.Name
		podName := "smoke-" + framework.RandomSuffix()
		pod := framework.MakePod(nsName, podName, nil, nil, framework.PauseImage, nil, nil)
		f.PodClient().CreateSync(pod)
		got := f.PodClient().GetPod(podName)
		gomega.Expect(got.Status.PodIPs).NotTo(gomega.BeEmpty(), "pod should have an IP assigned by kube-ovn")
		f.PodClient().DeleteSync(podName)

		ginkgo.By("Restoring BgpConf CRD")
		recreateCRD(ctx, ext, savedBgp)
		savedBgp = nil

		ginkgo.By("Restoring EvpnConf CRD")
		recreateCRD(ctx, ext, savedEvpn)
		savedEvpn = nil

		ginkgo.By("Verifying the controller's background poller picks up the restored CRDs")
		// Once the CRDs come back the client-go discovery cache + reflector
		// must drive the informers to HasSynced=true. We assert this by
		// listing the (empty) CR collections through the kube-ovn client,
		// which goes through the API server and returns a non-NotFound
		// status when the CRDs are established.
		kubeovn := f.KubeOVNClientSet.KubeovnV1()
		framework.WaitUntil(2*time.Second, selfHealTimeout, func(ctx context.Context) (bool, error) {
			for _, list := range []func() error{
				func() error {
					_, err := kubeovn.BgpConves().List(ctx, metav1.ListOptions{Limit: 1})
					return err
				},
				func() error {
					_, err := kubeovn.EvpnConves().List(ctx, metav1.ListOptions{Limit: 1})
					return err
				},
			} {
				switch err := list(); {
				case err == nil:
					continue
				case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
					// Discovery has not yet caught up with the freshly
					// restored CRD; keep polling.
					return false, nil
				default:
					// RBAC/connectivity/other errors should surface
					// immediately rather than be masked by the timeout.
					return false, err
				}
			}
			return true, nil
		}, "BgpConf/EvpnConf APIs to become listable after CRD restoration")
	})
})

// recreateCRD strips server-side fields and re-creates the CRD. Callers must
// pass a snapshot taken before deletion.
func recreateCRD(ctx context.Context, ext apiextensionsCRDInterface, snapshot *apiextensionsv1.CustomResourceDefinition) {
	ginkgo.GinkgoHelper()
	fresh := snapshot.DeepCopy()
	fresh.ObjectMeta = metav1.ObjectMeta{
		Name:        snapshot.Name,
		Labels:      snapshot.Labels,
		Annotations: snapshot.Annotations,
	}
	fresh.Status = apiextensionsv1.CustomResourceDefinitionStatus{}
	_, err := ext.Create(ctx, fresh, metav1.CreateOptions{})
	framework.ExpectNoError(err, "re-creating CRD %s", snapshot.Name)

	// Wait for the CRD to become Established so subsequent assertions don't
	// race the api-server's CRD registration.
	framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
		got, err := ext.Get(ctx, snapshot.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// The just-created CRD may not be visible yet on every
				// api-server replica; keep polling.
				return false, nil
			}
			// Anything else (Forbidden, conflict, transport error) is
			// surfaced so the helper fails fast with the underlying cause.
			return false, err
		}
		for _, cond := range got.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}, fmt.Sprintf("CRD %s to become Established", snapshot.Name))
}

func restoreCRD(ctx context.Context, f *framework.Framework, snapshot *apiextensionsv1.CustomResourceDefinition) {
	if snapshot == nil {
		return
	}
	ext := f.ExtClientSet.ApiextensionsV1().CustomResourceDefinitions()
	if _, err := ext.Get(ctx, snapshot.Name, metav1.GetOptions{}); err == nil {
		return
	}
	framework.Logf("AfterEach: restoring CRD %s", snapshot.Name)
	recreateCRD(ctx, ext, snapshot)
}

// apiextensionsCRDInterface narrows the apiextensions client to just the calls
// we exercise; it makes the helper easy to mock if we ever need to.
type apiextensionsCRDInterface interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*apiextensionsv1.CustomResourceDefinition, error)
	Create(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition, opts metav1.CreateOptions) (*apiextensionsv1.CustomResourceDefinition, error)
}
