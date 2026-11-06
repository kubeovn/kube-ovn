package controller

import (
	"sync"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadfake "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/fake"
	nadinformers "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/informers/externalversions"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	corefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformers "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
)

const fakeInformerTimeout = 5 * time.Second

type fakeInformerWatchKey struct {
	resource  schema.GroupVersionResource
	namespace string
}

// trackFakeInformerWatches is checked after the initial informer cache sync.
// A fake tracker cannot replay deletes between LIST and WATCH, so cache sync
// alone does not make it safe for tests to start deleting objects.
func trackFakeInformerWatches(client *clienttesting.Fake) cache.InformerSynced {
	var watches sync.Map
	client.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		watches.LoadOrStore(fakeInformerWatchKey{action.GetResource(), action.GetNamespace()}, false)
		return false, nil, nil
	})
	// Wrap the existing reactors to preserve each generated client's watch
	// options and only mark a watch ready after its tracker has registered it.
	for i, reactor := range client.WatchReactionChain {
		client.WatchReactionChain[i] = &clienttesting.SimpleWatchReactor{
			Resource: "*",
			Reaction: func(action clienttesting.Action) (bool, watch.Interface, error) {
				if !reactor.Handles(action) {
					return false, nil, nil
				}
				handled, watcher, err := reactor.React(action)
				if handled && watcher != nil && err == nil {
					watches.Store(fakeInformerWatchKey{action.GetResource(), action.GetNamespace()}, true)
				}
				return handled, watcher, err
			},
		}
	}
	return func() bool {
		synced := true
		watches.Range(func(_, ready any) bool {
			synced = ready.(bool)
			return synced
		})
		return synced
	}
}

func TestFakeInformerWatchSync(t *testing.T) {
	for _, resource := range []string{"ips", "vlans", "services", "network-attachment-definitions"} {
		t.Run(resource, func(t *testing.T) {
			var client *clienttesting.Fake
			var informer cache.SharedIndexInformer
			var remove func() error
			key := "audit"
			switch resource {
			case "ips":
				c := kubeovnfake.NewSimpleClientset(&kubeovnv1.IP{Name: key})
				client = &c.Fake
				informer = kubeovninformers.NewSharedInformerFactory(c, 0).Kubeovn().V1().IPs().Informer()
				remove = func() error { return c.KubeovnV1().IPs().Delete(t.Context(), "audit", metav1.DeleteOptions{}) }
			case "vlans":
				c := kubeovnfake.NewSimpleClientset(&kubeovnv1.Vlan{Name: key})
				client = &c.Fake
				informer = kubeovninformers.NewSharedInformerFactory(c, 0).Kubeovn().V1().Vlans().Informer()
				remove = func() error { return c.KubeovnV1().Vlans().Delete(t.Context(), "audit", metav1.DeleteOptions{}) }
			case "services":
				c := corefake.NewClientset(&corev1.Service{Name: key, Namespace: metav1.NamespaceDefault})
				client = &c.Fake
				informer = informers.NewSharedInformerFactory(c, 0).Core().V1().Services().Informer()
				remove = func() error {
					return c.CoreV1().Services(metav1.NamespaceDefault).Delete(t.Context(), "audit", metav1.DeleteOptions{})
				}
				key = metav1.NamespaceDefault + "/" + key
			case "network-attachment-definitions":
				// The NAD fake uses a hyphenated resource name that tracker.Add
				// cannot infer from the kind, so seed it through the API.
				c := nadfake.NewSimpleClientset()
				_, err := c.K8sCniCncfIoV1().NetworkAttachmentDefinitions(metav1.NamespaceDefault).Create(
					t.Context(), &nadv1.NetworkAttachmentDefinition{Name: key, Namespace: metav1.NamespaceDefault}, metav1.CreateOptions{},
				)
				require.NoError(t, err)
				client = &c.Fake
				informer = nadinformers.NewSharedInformerFactory(c, 0).K8sCniCncfIo().V1().NetworkAttachmentDefinitions().Informer()
				remove = func() error {
					return c.K8sCniCncfIoV1().NetworkAttachmentDefinitions(metav1.NamespaceDefault).Delete(t.Context(), "audit", metav1.DeleteOptions{})
				}
				key = metav1.NamespaceDefault + "/" + key
			}
			watchRequested, allowWatch := make(chan struct{}), make(chan struct{})
			requested := sync.OnceFunc(func() { close(watchRequested) })
			release := sync.OnceFunc(func() { close(allowWatch) })
			client.PrependWatchReactor(resource, func(clienttesting.Action) (bool, watch.Interface, error) {
				requested()
				<-allowWatch
				return false, nil, nil
			})
			watchesSynced := trackFakeInformerWatches(client)
			var wg sync.WaitGroup
			wg.Go(func() { informer.Run(t.Context().Done()) })
			t.Cleanup(func() { release(); wg.Wait() })
			require.Eventually(t, informer.HasSynced, fakeInformerTimeout, time.Millisecond)
			select {
			case <-watchRequested:
			case <-time.After(fakeInformerTimeout):
				t.Fatal("informer did not request a watch")
			}
			_, exists, err := informer.GetIndexer().GetByKey(key)
			require.NoError(t, err)
			require.True(t, exists)
			require.False(t, watchesSynced(), "initial cache sync must not imply WATCH readiness")
			release()
			require.Eventually(t, watchesSynced, fakeInformerTimeout, time.Millisecond)
			require.NoError(t, remove())
			require.Eventually(t, func() bool {
				_, exists, err := informer.GetIndexer().GetByKey(key)
				return err == nil && !exists
			}, fakeInformerTimeout, time.Millisecond, "deletion after WATCH readiness must reach the cache")
		})
	}
}
