package ipsec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	certv1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	NodeNameAnnotation = "kube-ovn.io/ipsec-node"
	NodeUIDAnnotation  = "kube-ovn.io/ipsec-node-uid"
)

type issuer struct {
	kube                             kubernetes.Interface
	node, nodeUID, podUID, namespace string
	trustHash                        string
	duration                         time.Duration
}

func requestName(nodeUID string, csr []byte) string {
	return "ovn-ipsec-" + digest(append([]byte(nodeUID+":"), csr...))[:48]
}

func (i issuer) name(csr []byte) string {
	// A rebuilt Pod has a different authenticated identity even when its
	// pending key survives. Never reuse a CSR bound to the deleted Pod.
	identity := fmt.Sprintf("%s:%s:%s:%s", i.nodeUID, i.podUID, i.duration, i.trustHash)
	return requestName(identity, csr)
}

func (i issuer) sign(ctx context.Context, csr []byte) ([]byte, error) {
	client := i.kube.CertificatesV1().CertificateSigningRequests()
	seconds := int64(i.duration / time.Second)
	if seconds < 600 || seconds > math.MaxInt32 {
		return nil, errors.New("invalid IPsec certificate duration")
	}
	req := &certv1.CertificateSigningRequest{
		Name: i.name(csr), Annotations: map[string]string{NodeNameAnnotation: i.node, NodeUIDAnnotation: i.nodeUID},
		Spec: certv1.CertificateSigningRequestSpec{Request: csr, SignerName: util.SignerName, Usages: []certv1.KeyUsage{certv1.UsageIPsecTunnel}, ExpirationSeconds: new(int32(seconds))},
	}
	created, err := client.Create(ctx, req, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		created, err = client.Get(ctx, req.Name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(created.Spec.Request, csr) || created.Spec.SignerName != req.Spec.SignerName || !slices.Equal(created.Spec.Usages, req.Spec.Usages) || created.Spec.ExpirationSeconds == nil || *created.Spec.ExpirationSeconds != *req.Spec.ExpirationSeconds || created.Annotations[NodeNameAnnotation] != i.node || created.Annotations[NodeUIDAnnotation] != i.nodeUID {
		return nil, errors.New("IPsec CSR name conflicts with a different request")
	}
	if created.Spec.Username != "system:serviceaccount:"+i.namespace+":kube-ovn-cni" || !slices.Equal(created.Spec.Extra["authentication.kubernetes.io/pod-uid"], []string{i.podUID}) {
		return nil, errors.New("IPsec CSR is not authenticated as the current bound Pod")
	}
	check := func(event watch.Event) (bool, error) {
		if event.Type == watch.Deleted {
			return false, errors.New("IPsec CSR deleted while waiting for signing")
		}
		obj, ok := event.Object.(*certv1.CertificateSigningRequest)
		if !ok {
			return false, nil
		}
		if obj.UID != created.UID {
			return false, errors.New("IPsec CSR UID changed")
		}
		for _, condition := range obj.Status.Conditions {
			if condition.Status == "True" && (condition.Type == certv1.CertificateDenied || condition.Type == certv1.CertificateFailed) {
				return false, fmt.Errorf("IPsec CSR %s: %s: %s", condition.Type, condition.Reason, condition.Message)
			}
		}
		return len(obj.Status.Certificate) != 0, nil
	}
	if done, err := check(watch.Event{Type: watch.Added, Object: created}); done || err != nil {
		return created.Status.Certificate, err
	}
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = "metadata.name=" + req.Name
			return client.List(ctx, opts)
		},
		WatchFuncWithContext: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = "metadata.name=" + req.Name
			return client.Watch(ctx, opts)
		},
	}
	event, err := watchtools.UntilWithSync(ctx, lw, &certv1.CertificateSigningRequest{}, nil, check)
	if err != nil {
		return nil, fmt.Errorf("wait for IPsec CSR: %w", err)
	}
	return event.Object.(*certv1.CertificateSigningRequest).Status.Certificate, nil
}
