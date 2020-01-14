package framework

import (
	"fmt"
	v1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/alauda/kube-ovn/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
)

type Framework struct {
	BaseName string
	KubeOvnNamespace string
	KubeClientSet kubernetes.Interface
	OvnClientSet  clientset.Interface
}

func NewFramework(baseName, kubeConfig string) *Framework {
	f := &Framework{BaseName: baseName}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		panic(err.Error())
	}

	cfg.QPS = 1000
	cfg.Burst = 2000
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err.Error())
	}

	f.KubeClientSet = kubeClient

	kubeOvnClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		panic(err.Error())
	}

	f.OvnClientSet = kubeOvnClient
	return f
}

func (f *Framework) GetName() string {
	return strings.Replace(CurrentGinkgoTestDescription().TestText, " ", "-", -1)
}

func (f *Framework) WaitSubnetReady(subnet string) error {
	for {
		s, err := f.OvnClientSet.KubeovnV1().Subnets().Get(subnet, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if s.Status.IsReady() {
			return nil
		}
		if s.Status.IsNotValidated() && s.Status.ConditionReason(v1.Validated) != "" {
			return fmt.Errorf(s.Status.ConditionReason(v1.Validated))
		}
		time.Sleep(1 * time.Second)
	}
}

func (f *Framework) WaitPodReady(pod, namespace string) error {
	for {
		p, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(pod, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if p.Status.Phase == "Running" && p.Status.Reason != "" {
			return nil
		}


		switch getPodStatus(p) {
		case Completed:
			return fmt.Errorf("pod already completed")
		case Running:
			return nil
		case Initing, Pending, PodInitializing, ContainerCreating, Terminating:
			continue
		default:
			fmt.Printf("%v", p.String())
			return fmt.Errorf("pod status failed")
		}
	}
}

const (
	Running           = "Running"
	Pending           = "Pending"
	Completed         = "Completed"
	ContainerCreating = "ContainerCreating"
	PodInitializing   = "PodInitializing"
	Terminating       = "Terminating"
	Initing           = "Initing"
)

func getPodContainerStatus(pod *corev1.Pod, reason string) string {
	for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
		container := pod.Status.ContainerStatuses[i]

		if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
			reason = container.State.Waiting.Reason
		} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
			reason = container.State.Terminated.Reason
		} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
			if container.State.Terminated.Signal != 0 {
				reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
			} else {
				reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
			}
		}
	}
	return reason
}

func getPodStatus(pod *corev1.Pod) string {
	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}
	initializing, reason := getPodInitStatus(pod, reason)
	if !initializing {
		reason = getPodContainerStatus(pod, reason)
	}

	if pod.DeletionTimestamp != nil && pod.Status.Reason == "NodeLost" {
		reason = "Unknown"
	} else if pod.DeletionTimestamp != nil {
		reason = "Terminating"
	}
	return reason
}

func getPodInitStatus(pod *corev1.Pod, reason string) (bool, string) {
	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0:
			continue
		case container.State.Terminated != nil:
			// initialization is failed
			if len(container.State.Terminated.Reason) == 0 {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Init:Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("Init:ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else {
				reason = "Init:" + container.State.Terminated.Reason
			}
			initializing = true
		case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
			reason = "Initing:" + container.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Initing:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}
	return initializing, reason
}
