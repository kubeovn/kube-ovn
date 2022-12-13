package framework

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func GetKubeOvnImage(cs clientset.Interface) string {
	ds, err := cs.AppsV1().DaemonSets(KubeOvnNamespace).Get(context.TODO(), DaemonSetOvsOvn, metav1.GetOptions{})
	ExpectNoError(err, "getting daemonset %s/%s", KubeOvnNamespace, DaemonSetOvsOvn)
	return ds.Spec.Template.Spec.Containers[0].Image
}

func GetOvsPodOnNode(cs clientset.Interface, node string) *corev1.Pod {
	ds, err := cs.AppsV1().DaemonSets(KubeOvnNamespace).Get(context.TODO(), DaemonSetOvsOvn, metav1.GetOptions{})
	ExpectNoError(err, "getting daemonset %s/%s", KubeOvnNamespace, DaemonSetOvsOvn)
	ovsPod, err := GetPodOnNodeForDaemonSet(cs, ds, node)
	ExpectNoError(err, "getting daemonset %s/%s running on node %s", KubeOvnNamespace, DaemonSetOvsOvn, node)
	return ovsPod
}
