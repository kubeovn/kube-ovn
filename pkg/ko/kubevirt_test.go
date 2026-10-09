package ko

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func captureVMI() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kubevirt.io/v1", "kind": "VirtualMachineInstance",
		"metadata": map[string]any{"name": "vm-a", "namespace": "app", "uid": "vmi-id"},
		"status":   map[string]any{"phase": "Running", "nodeName": "node-a"},
	}}
}

func captureLauncher(name, node string) *corev1.Pod {
	return &corev1.Pod{
		Name: name, Namespace: "app", UID: types.UID(name + "-uid"),
		Labels:          map[string]string{"kubevirt.io": "virt-launcher", "kubevirt.io/created-by": "vmi-id"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "kubevirt.io/v1", Kind: "VirtualMachineInstance", Name: "vm-a", UID: "vmi-id", Controller: new(true)}},
		Spec:            corev1.PodSpec{NodeName: node}, Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestVMILauncherMigrationSelection(t *testing.T) {
	for _, tc := range []struct {
		name, node, expected string
		migration            map[string]any
	}{
		{"no migration", "node-a", "source", nil},
		{"in progress", "node-a", "source", map[string]any{"sourcePod": "source", "targetPod": "target"}},
		{"explicit false", "node-a", "source", map[string]any{"sourcePod": "source", "targetPod": "target", "completed": false}},
		{"completed", "node-b", "target", map[string]any{"sourcePod": "source", "targetPod": "target", "completed": true}},
		{"failed", "node-a", "source", map[string]any{"sourcePod": "source", "targetPod": "target", "completed": true, "failed": true}},
		{"missing Pod names", "node-a", "source", map[string]any{"completed": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vmi := captureVMI()
			status := vmi.Object["status"].(map[string]any)
			status["nodeName"] = tc.node
			status["activePods"] = map[string]any{"source-uid": "node-a", "target-uid": "node-b"}
			if tc.migration != nil {
				status["migrationState"] = tc.migration
			}
			app, _, _, _ := testApplication(t, captureLauncher("source", "node-a"), captureLauncher("target", "node-b"))
			client, err := app.newClient()
			require.NoError(t, err)
			pod, err := client.vmiLauncher(t.Context(), vmi)
			require.NoError(t, err)
			require.Equal(t, tc.expected, pod.Name)
		})
	}
}

func TestVMILauncherRejectsInvalidCandidates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*unstructured.Unstructured, *corev1.Pod) []*corev1.Pod
	}{
		{"terminating", func(_ *unstructured.Unstructured, p *corev1.Pod) []*corev1.Pod {
			p.DeletionTimestamp = new(metav1.Now())
			return nil
		}},
		{"pending", func(_ *unstructured.Unstructured, p *corev1.Pod) []*corev1.Pod {
			p.Status.Phase = corev1.PodPending
			return nil
		}},
		{"stale owner", func(_ *unstructured.Unstructured, p *corev1.Pod) []*corev1.Pod {
			p.OwnerReferences[0].UID = "old-vmi"
			return nil
		}},
		{"not controller", func(_ *unstructured.Unstructured, p *corev1.Pod) []*corev1.Pod {
			p.OwnerReferences[0].Controller = new(false)
			return nil
		}},
		{"wrong node", func(_ *unstructured.Unstructured, p *corev1.Pod) []*corev1.Pod {
			p.Spec.NodeName = "node-b"
			return nil
		}},
		{"inactive Pod", func(v *unstructured.Unstructured, _ *corev1.Pod) []*corev1.Pod {
			v.Object["status"].(map[string]any)["activePods"] = map[string]any{"stale-uid": "node-a"}
			return nil
		}},
		{"ambiguous", func(_ *unstructured.Unstructured, _ *corev1.Pod) []*corev1.Pod {
			return []*corev1.Pod{captureLauncher("other", "node-a")}
		}},
		{"missing migration Pod", func(v *unstructured.Unstructured, _ *corev1.Pod) []*corev1.Pod {
			v.Object["status"].(map[string]any)["migrationState"] = map[string]any{"sourcePod": "missing", "completed": false}
			return nil
		}},
		{"stopped VMI", func(v *unstructured.Unstructured, _ *corev1.Pod) []*corev1.Pod {
			v.Object["status"].(map[string]any)["phase"] = "Succeeded"
			return nil
		}},
		{"malformed state", func(v *unstructured.Unstructured, _ *corev1.Pod) []*corev1.Pod {
			v.Object["status"].(map[string]any)["migrationState"] = map[string]any{"completed": "false"}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vmi, pod := captureVMI(), captureLauncher("source", "node-a")
			objects := []runtime.Object{pod}
			for _, extra := range tc.mutate(vmi, pod) {
				objects = append(objects, extra)
			}
			app, _, _, _ := testApplication(t, objects...)
			client, err := app.newClient()
			require.NoError(t, err)
			_, err = client.vmiLauncher(t.Context(), vmi)
			require.Error(t, err)
		})
	}
}

func TestVMILauncherFiltersStaleAndConcurrentPods(t *testing.T) {
	for _, migration := range []bool{false, true} {
		t.Run(map[bool]string{false: "active Pod UID", true: "migration Pod name"}[migration], func(t *testing.T) {
			vmi := captureVMI()
			status := vmi.Object["status"].(map[string]any)
			if migration {
				status["migrationState"] = map[string]any{"completed": true, "sourcePod": "source", "targetPod": "target"}
			} else {
				status["activePods"] = map[string]any{"target-uid": "node-a"}
			}
			stale := captureLauncher("stale", "node-a")
			stale.OwnerReferences[0].UID = "old-vmi"
			app, _, _, _ := testApplication(t, captureLauncher("source", "node-a"), captureLauncher("target", "node-a"), stale)
			client, err := app.newClient()
			require.NoError(t, err)
			pod, err := client.vmiLauncher(t.Context(), vmi)
			require.NoError(t, err)
			require.Equal(t, "target", pod.Name)
		})
	}
}

func TestCaptureKubeVirtPreservesBinaryOutput(t *testing.T) {
	for _, kind := range []string{"vm", "vmi"} {
		for _, qualified := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "context namespace", true: "explicit namespace"}[qualified], func(t *testing.T) {
				vmi := captureVMI()
				vmi.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "kubevirt.io/v1", Kind: "VirtualMachine", Name: "vm-a", UID: "vm-id", Controller: new(true)}})
				vm := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "kubevirt.io/v1", "kind": "VirtualMachine", "metadata": map[string]any{"name": "vm-a", "namespace": "app", "uid": "vm-id"},
				}}
				pod := captureLauncher("source", "node-a")
				agent := readyPod("agent-a", "node-a", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
				app, executor, out, stderr := testApplication(t, pod, agent, &corev1.Node{Name: "node-a"})
				client, err := app.newClient()
				require.NoError(t, err)
				client.Dynamic = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), vm, vmi)
				client.ComponentFree = true
				reference := "vm-a"
				if qualified {
					reference = "app/vm-a"
					client.WorkloadNamespace = "wrong-namespace"
				}
				executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
					if argv[0] == "ovs-vsctl" {
						require.Equal(t, `external_ids:iface-id="vm-a.app"`, argv[len(argv)-1])
						_, err := io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[["nic-a",["map",[["pod_netns","/var/run/netns/vm-a"]]],4]]}`)
						return err
					}
					_, err := streams.Out.Write([]byte{0, 255, 10, 13, 0})
					return err
				}
				require.NoError(t, app.Execute(t.Context(), []string{"capture", "--" + kind, reference, "--", "-w", "-", "-c", "1"}))
				require.Equal(t, []byte{0, 255, 10, 13, 0}, out.Bytes())
				require.Equal(t, "agent", executor.calls[1].target.Container)
				require.Contains(t, stderr.String(), "VMI app/vm-a through Pod source on node node-a")
				require.Equal(t, []string{"nsenter", "--net=/var/run/netns/vm-a", "--", "tcpdump", "-nn", "-i", "eth0", "-w", "-", "-c", "1"}, executor.calls[1].argv)
			})
		}
	}
}

func TestCaptureKubeVirtSurfacesAPIErrors(t *testing.T) {
	for _, verb := range []string{"get", "list", "get VM", "not found"} {
		t.Run(verb, func(t *testing.T) {
			app, executor, _, _ := testApplication(t)
			client, err := app.newClient()
			require.NoError(t, err)
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), captureVMI())
			client.Dynamic = dynamic
			reactor := func(k8stesting.Action) (bool, runtime.Object, error) {
				if verb == "not found" {
					return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "virtualmachineinstances"}, "vm-a")
				}
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "capture"}, "vm-a", io.ErrUnexpectedEOF)
			}
			kind := "vmi"
			switch verb {
			case "get", "not found":
				dynamic.PrependReactor("get", "virtualmachineinstances", reactor)
			case "get VM":
				kind = "vm"
				dynamic.PrependReactor("get", "virtualmachines", reactor)
			case "list":
				client.Kubernetes.(*fake.Clientset).PrependReactor("list", "pods", reactor)
			}
			err = app.Execute(t.Context(), []string{"capture", "--" + kind, "app/vm-a"})
			if verb == "not found" {
				require.True(t, apierrors.IsNotFound(err), "%v", err)
			} else {
				require.True(t, apierrors.IsForbidden(err), "%v", err)
			}
			require.Empty(t, executor.calls)
		})
	}
}

func TestCaptureRejectsVMOwnershipAndVMIRaces(t *testing.T) {
	for _, mode := range []string{"wrong VM owner", "replaced VMI", "migrated VMI", "replaced Pod"} {
		t.Run(mode, func(t *testing.T) {
			vmi, pod := captureVMI(), captureLauncher("source", "node-a")
			app, _, _, _ := testApplication(t, pod)
			client, err := app.newClient()
			require.NoError(t, err)
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), vmi)
			client.Dynamic = dynamic
			if mode == "wrong VM owner" {
				vm := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "kubevirt.io/v1", "kind": "VirtualMachine", "metadata": map[string]any{"name": "vm-a", "namespace": "app", "uid": "vm-id"}}}
				dynamic.PrependReactor("get", "virtualmachines", func(k8stesting.Action) (bool, runtime.Object, error) { return true, vm, nil })
				_, err = client.captureTarget(t.Context(), captureOptions{vm: "vm-a"})
				require.ErrorContains(t, err, "not controlled by the current VM")
				return
			}
			target, err := client.captureTarget(t.Context(), captureOptions{vmi: "vm-a"})
			require.NoError(t, err)
			switch mode {
			case "replaced VMI":
				vmi.SetUID("new-vmi")
			case "migrated VMI":
				vmi.Object["status"].(map[string]any)["nodeName"] = "node-b"
			case "replaced Pod":
				pod.UID = "new-pod"
				_, err = client.Kubernetes.CoreV1().Pods("app").Update(t.Context(), pod, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			_, err = dynamic.Resource(vmiResource).Namespace("app").Update(t.Context(), vmi, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.Error(t, client.checkCaptureTarget(t.Context(), target))
		})
	}
}
