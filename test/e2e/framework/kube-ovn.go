package framework

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func GetKubeOvnImage(cs clientset.Interface) string {
	ds, err := cs.AppsV1().DaemonSets(KubeOvnNamespace).Get(context.TODO(), DaemonSetOvsOvn, metav1.GetOptions{})
	ExpectNoError(err, "getting daemonset %s/%s", KubeOvnNamespace, DaemonSetOvsOvn)
	return ds.Spec.Template.Spec.Containers[0].Image
}
