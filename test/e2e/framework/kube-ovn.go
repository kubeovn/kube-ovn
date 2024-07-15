package framework

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func GetKubeOvnImage(ctx context.Context, cs clientset.Interface) string {
	ginkgo.GinkgoHelper()
	ds, err := cs.AppsV1().DaemonSets(KubeOvnNamespace).Get(ctx, DaemonSetOvsOvn, metav1.GetOptions{})
	ExpectNoError(err, "getting daemonset %s/%s", KubeOvnNamespace, DaemonSetOvsOvn)
	ExpectNotNil(ds, "daemonset %s/%s not found", KubeOvnNamespace, DaemonSetOvsOvn)
	return ds.Spec.Template.Spec.Containers[0].Image
}
