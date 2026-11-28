package connectivity

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReplacementPodReady(t *testing.T) {
	ready := &corev1.Pod{
		UID: "replacement",
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "openvswitch", Ready: true}},
		},
	}
	testCases := []struct {
		name   string
		mutate func(*corev1.Pod)
		want   bool
	}{
		{"ready replacement", func(*corev1.Pod) {}, true},
		// The failed CI run selected the deleted, still-ready Pod as its replacement.
		{"deleted pod still ready", func(p *corev1.Pod) {
			p.UID = "deleted"
			p.DeletionTimestamp = new(metav1.Now())
		}, false},
		{"stale deleted pod before termination is observed", func(p *corev1.Pod) {
			p.UID = "deleted"
		}, false},
		{"replacement already terminating", func(p *corev1.Pod) {
			p.DeletionTimestamp = new(metav1.Now())
		}, false},
		{"container ready but pod not ready", func(p *corev1.Pod) {
			p.Status.Conditions[0].Status = corev1.ConditionFalse
		}, false},
		{"replacement pending", func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodPending
		}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pod := ready.DeepCopy()
			tc.mutate(pod)
			if got := isReplacementPodReady(pod, "deleted"); got != tc.want {
				t.Errorf("isReplacementPodReady() = %t, want %t", got, tc.want)
			}
		})
	}
}
