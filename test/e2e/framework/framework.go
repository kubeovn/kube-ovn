package framework

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"

	. "github.com/onsi/ginkgo"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
)

type Framework struct {
	BaseName         string
	KubeOvnNamespace string
	KubeClientSet    kubernetes.Interface
	OvnClientSet     clientset.Interface
	KubeConfig       *rest.Config
}

func NewFramework(baseName, kubeConfig string) *Framework {
	f := &Framework{BaseName: baseName}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		panic(err.Error())
	}
	f.KubeConfig = cfg

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

func (f *Framework) WaitProviderNetworkReady(providerNetwork string) error {
	for {
		time.Sleep(1 * time.Second)

		pn, err := f.OvnClientSet.KubeovnV1().ProviderNetworks().Get(context.Background(), providerNetwork, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pn.Status.Ready {
			return nil
		}
	}
}

func (f *Framework) WaitSubnetReady(subnet string) error {
	for {
		time.Sleep(1 * time.Second)
		s, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), subnet, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if s.Status.IsReady() {
			return nil
		}
		if s.Status.IsNotValidated() && s.Status.ConditionReason(v1.Validated) != "" {
			return fmt.Errorf(s.Status.ConditionReason(v1.Validated))
		}
	}
}

func (f *Framework) WaitPodReady(pod, namespace string) (*corev1.Pod, error) {
	for {
		time.Sleep(1 * time.Second)
		p, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), pod, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if p.Status.Phase == "Running" && p.Status.Reason != "" {
			return p, nil
		}

		switch getPodStatus(*p) {
		case Completed:
			return nil, fmt.Errorf("pod already completed")
		case Running:
			return p, nil
		case Initing, Pending, PodInitializing, ContainerCreating, Terminating:
			continue
		default:
			klog.Info(p.String())
			return nil, fmt.Errorf("pod status failed")
		}
	}
}

func (f *Framework) WaitPodDeleted(pod, namespace string) error {
	for {
		time.Sleep(1 * time.Second)
		p, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), pod, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		if status := getPodStatus(*p); status != Terminating {
			return fmt.Errorf("unexpected pod status: %s", status)
		}
	}
}

func (f *Framework) WaitDeploymentReady(deployment, namespace string) error {
	for {
		time.Sleep(1 * time.Second)
		deploy, err := f.KubeClientSet.AppsV1().Deployments(namespace).Get(context.Background(), deployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if deploy.Status.ReadyReplicas != *deploy.Spec.Replicas {
			continue
		}

		pods, err := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deploy.Spec.Template.Labels).String()})
		if err != nil {
			return err
		}

		ready := true
		for _, pod := range pods.Items {
			switch getPodStatus(pod) {
			case Completed:
				return fmt.Errorf("pod already completed")
			case Running:
				continue
			case Initing, Pending, PodInitializing, ContainerCreating, Terminating:
				ready = false
			default:
				klog.Info(pod.String())
				return fmt.Errorf("pod status failed")
			}
		}
		if ready {
			return nil
		}
	}
}

func (f *Framework) WaitStatefulsetReady(statefulset, namespace string) error {
	for {
		time.Sleep(1 * time.Second)
		ss, err := f.KubeClientSet.AppsV1().StatefulSets(namespace).Get(context.Background(), statefulset, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if ss.Status.ReadyReplicas != *ss.Spec.Replicas {
			continue
		}

		pods, err := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(ss.Spec.Template.Labels).String()})
		if err != nil {
			return err
		}

		ready := true
		for _, pod := range pods.Items {
			switch getPodStatus(pod) {
			case Completed:
				return fmt.Errorf("pod already completed")
			case Running:
				continue
			case Initing, Pending, PodInitializing, ContainerCreating, Terminating:
				ready = false
			default:
				klog.Info(pod.String())
				return fmt.Errorf("pod status failed")
			}
		}
		if ready {
			return nil
		}
	}
}

func (f *Framework) ExecToPodThroughAPI(command, containerName, podName, namespace string, stdin io.Reader) (string, string, error) {
	req := f.KubeClientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return "", "", fmt.Errorf("error adding to scheme: %v", err)
	}

	parameterCodec := runtime.NewParameterCodec(scheme)
	req.VersionedParams(&corev1.PodExecOptions{
		Command:   strings.Fields(command),
		Container: containerName,
		Stdin:     stdin != nil,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(f.KubeConfig, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("error while creating Executor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	if err != nil {
		return "", "", fmt.Errorf("error in Stream: %v", err)
	}

	return stdout.String(), stderr.String(), nil
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

func getPodContainerStatus(pod corev1.Pod, reason string) string {
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

func getPodStatus(pod corev1.Pod) string {
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

func getPodInitStatus(pod corev1.Pod, reason string) (bool, string) {
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
