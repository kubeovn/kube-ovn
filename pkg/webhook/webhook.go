package webhook

import (
	"context"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/ovs"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	createHooks = make(map[metav1.GroupVersionKind]admission.HandlerFunc)
	updateHooks = make(map[metav1.GroupVersionKind]admission.HandlerFunc)
	deleteHooks = make(map[metav1.GroupVersionKind]admission.HandlerFunc)
)

type ValidatingHook struct {
	client        client.Client
	decoder       *admission.Decoder
	ovnClient     *ovs.Client
	kubeclientset kubernetes.Interface
	opt           *WebhookOptions
	cache         cache.Cache
}

type WebhookOptions struct {
	OvnNbHost    string
	OvnNbPort    int
	OvnNbTimeout int
	DefaultLS    string
}

func NewValidatingHook(c cache.Cache, opt *WebhookOptions) (*ValidatingHook, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		klog.Errorf("use in cluster config failed %v", err)
		return nil, err
	}
	cfg.Timeout = 15 * time.Second
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return nil, err
	}

	v := &ValidatingHook{
		kubeclientset: kubeClient,
		ovnClient:     ovs.NewClient(opt.OvnNbHost, opt.OvnNbTimeout, "", "", "", "", "", "", "", ""),
		opt:           opt,
		cache:         c,
	}

	// initialize hook handlers mapping
	createHooks[deploymentGVK] = v.DeploymentCreateHook
	createHooks[statefulSetGVK] = v.StatefulSetCreateHook
	createHooks[daemonSetGVK] = v.DaemonSetCreateHook
	createHooks[podGVK] = v.PodCreateHook

	updateHooks[deploymentGVK] = v.DeploymentUpdateHook
	updateHooks[statefulSetGVK] = v.StatefulSetUpdateHook
	updateHooks[daemonSetGVK] = v.DaemonSetUpdateHook

	deleteHooks[deploymentGVK] = v.DeploymentDeleteHook
	deleteHooks[statefulSetGVK] = v.StatefulSetDeleteHook
	deleteHooks[daemonSetGVK] = v.DaemonSetDeleteHook
	deleteHooks[podGVK] = v.PodDeleteHook

	return v, nil
}

func (v *ValidatingHook) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	defer func() {
		if resp.AdmissionResponse.Allowed {
			klog.V(3).Info("result: allowed")
		} else {
			klog.V(3).Infof("result: reject, reason: %s", resp.AdmissionResponse.Result.Reason)
		}
	}()

	switch req.Operation {
	case admissionv1.Create:
		if createHooks[req.Kind] != nil {
			klog.Infof("handle create %s %s@%s", req.Kind, req.Name, req.Namespace)
			resp = createHooks[req.Kind](ctx, req)
			return
		}
	case admissionv1.Update:
		if updateHooks[req.Kind] != nil {
			klog.Infof("handle update %s %s@%s", req.Kind, req.Name, req.Namespace)
			resp = updateHooks[req.Kind](ctx, req)
			return
		}
	case admissionv1.Delete:
		if deleteHooks[req.Kind] != nil {
			klog.Infof("handle delete %s %s@%s", req.Kind, req.Name, req.Namespace)
			resp = deleteHooks[req.Kind](ctx, req)
			return
		}
	}
	resp = ctrlwebhook.Allowed("by pass")
	return
}

func (v *ValidatingHook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

func (v *ValidatingHook) InjectClient(c client.Client) error {
	v.client = c
	return nil
}
