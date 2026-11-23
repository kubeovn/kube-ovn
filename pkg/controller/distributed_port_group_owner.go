package controller

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const distributedPortOwnerConcurrency = 8

type distributedPortOwnerSnapshot struct {
	pods   map[string]*v1.Pod
	vmPods map[string][]*v1.Pod
	vmis   map[string]*kubevirtv1.VirtualMachineInstance
}

// The caller holds the membership lock. Never reuse these reads across batches:
// the same retained LSP may be claimed by a new Pod between two writes.
func (c *Controller) loadDistributedPortOwners(ctx context.Context, ports []string, owners map[string]distributedPortOwner) (*distributedPortOwnerSnapshot, error) {
	snapshot := &distributedPortOwnerSnapshot{
		pods: make(map[string]*v1.Pod), vmPods: make(map[string][]*v1.Pod),
		vmis: make(map[string]*kubevirtv1.VirtualMachineInstance),
	}
	podNames, vmNames := make(map[string]string), make(map[string]string)
	for _, uuid := range ports {
		owner := owners[uuid]
		if owner.pod != nil {
			pod := owner.pod
			podNames[pod.Namespace+"/"+pod.Name] = pod.Namespace
			if isVM, _ := isVMPod(pod); isVM && c.config.EnableKeepVMIP {
				snapshot.vmPods[pod.Namespace] = nil
			}
		} else if ip := owner.retained; ip != nil {
			key := ip.Spec.Namespace + "/" + ip.Spec.PodName
			if ip.Spec.PodType == util.KindStatefulSet {
				podNames[key] = ip.Spec.Namespace
			} else {
				vmNames[key] = ip.Spec.Namespace
				snapshot.vmPods[ip.Spec.Namespace] = nil
			}
		} else {
			return nil, fmt.Errorf("missing owner of logical switch port %s", uuid)
		}
	}
	var checks []func(context.Context) error
	var mu sync.Mutex
	for namespace := range snapshot.vmPods {
		checks = append(checks, func(ctx context.Context) error {
			pods, err := c.config.KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			for i := range pods.Items {
				pod := &pods.Items[i]
				snapshot.pods[namespace+"/"+pod.Name] = pod
				snapshot.vmPods[namespace] = append(snapshot.vmPods[namespace], pod)
			}
			return nil
		})
	}
	for key, namespace := range podNames {
		if _, listed := snapshot.vmPods[namespace]; listed {
			continue
		}
		checks = append(checks, func(ctx context.Context) error {
			pod, err := c.config.KubeClient.CoreV1().Pods(namespace).Get(ctx, key[len(namespace)+1:], metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				pod = nil
			} else if err != nil {
				return err
			}
			mu.Lock()
			snapshot.pods[key] = pod
			mu.Unlock()
			return nil
		})
	}
	for key, namespace := range vmNames {
		checks = append(checks, func(ctx context.Context) error {
			vmi, err := c.config.KubevirtClient.VirtualMachineInstance(namespace).Get(ctx, key[len(namespace)+1:], metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				vmi = nil
			} else if err != nil {
				return err
			}
			mu.Lock()
			snapshot.vmis[key] = vmi
			mu.Unlock()
			return nil
		})
	}
	if err := checkDistributedPortOwners(ctx, checks); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func checkDistributedPortOwners(ctx context.Context, checks []func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errors := make(chan error, 1)
	workqueue.ParallelizeUntil(ctx, distributedPortOwnerConcurrency, len(checks), func(i int) {
		if err := checks[i](ctx); err != nil {
			select {
			case errors <- err:
				cancel()
			default:
			}
		}
	})
	select {
	case err := <-errors:
		return err
	default:
		return ctx.Err()
	}
}

func (c *Controller) distributedPortOwnerMatches(subnet string, owner distributedPortOwner, snapshot *distributedPortOwnerSnapshot) bool {
	if expected := owner.pod; expected != nil {
		pod := snapshot.pods[expected.Namespace+"/"+expected.Name]
		if !c.podPortOwnerMatches(pod, subnet, owner) {
			return false
		}
		portName := ovs.PodNameToPortName(c.getNameByPod(pod), pod.Namespace, owner.provider)
		if c.distributedPortProvider(pod, subnet, portName) != owner.provider {
			return false
		}
		if isVM, name := isVMPod(pod); isVM && c.config.EnableKeepVMIP {
			return !hasOtherAliveVMPortOwner(snapshot.vmPods[pod.Namespace], name, pod.Name)
		}
		return true
	}
	ip := owner.retained
	key := ip.Spec.Namespace + "/" + ip.Spec.PodName
	if ip.Spec.PodType == util.KindStatefulSet {
		pod := snapshot.pods[key]
		return pod == nil || !isPodAlive(pod)
	}
	vmi := snapshot.vmis[key]
	return (vmi == nil || vmi.IsFinal()) && !hasOtherAliveVMPortOwner(snapshot.vmPods[ip.Spec.Namespace], ip.Spec.PodName, "")
}
