package framework

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	v1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/typed/k8s.cni.cncf.io/v1"
)

// NetworkAttachmentDefinitionClient is a struct for nad client.
type NetworkAttachmentDefinitionClient struct {
	f *Framework
	v1.NetworkAttachmentDefinitionInterface
}

func (f *Framework) NetworkAttachmentDefinitionClient(namespace string) *NetworkAttachmentDefinitionClient {
	return &NetworkAttachmentDefinitionClient{
		f:                                    f,
		NetworkAttachmentDefinitionInterface: f.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(namespace),
	}
}

func (c *NetworkAttachmentDefinitionClient) Get(name string) *apiv1.NetworkAttachmentDefinition {
	nad, err := c.NetworkAttachmentDefinitionInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return nad
}

// Create creates a new nad according to the framework specifications
func (c *NetworkAttachmentDefinitionClient) Create(nad *apiv1.NetworkAttachmentDefinition) *apiv1.NetworkAttachmentDefinition {
	nad, err := c.NetworkAttachmentDefinitionInterface.Create(context.TODO(), nad, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating nad")
	return nad.DeepCopy()
}

// Delete deletes a nad if the nad exists
func (c *NetworkAttachmentDefinitionClient) Delete(name string) {
	err := c.NetworkAttachmentDefinitionInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	ExpectNoError(err, "Error deleting nad")
}

func MakeNetworkAttachmentDefinition(name, namespace, conf string) *apiv1.NetworkAttachmentDefinition {
	nad := &apiv1.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.NetworkAttachmentDefinitionSpec{
			Config: conf,
		},
	}
	return nad
}
