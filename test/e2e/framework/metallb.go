package framework

import (
	"context"

	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type MetallbClientSet struct {
	client *rest.RESTClient
}

func NewMetallbClientSet(config *rest.Config) (*MetallbClientSet, error) {
	if err := metallbv1beta1.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}
	config.GroupVersion = &metallbv1beta1.GroupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	client, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	return &MetallbClientSet{client: client}, nil
}

func (c *MetallbClientSet) CreateIPAddressPool(pool *metallbv1beta1.IPAddressPool) (*metallbv1beta1.IPAddressPool, error) {
	result := &metallbv1beta1.IPAddressPool{}
	err := c.client.Post().
		Namespace("metallb-system").
		Resource("ipaddresspools").
		Body(pool).
		Do(context.TODO()).
		Into(result)
	return result, err
}

func (c *MetallbClientSet) CreateL2Advertisement(advertisement *metallbv1beta1.L2Advertisement) (*metallbv1beta1.L2Advertisement, error) {
	result := &metallbv1beta1.L2Advertisement{}
	err := c.client.Post().
		Namespace("metallb-system").
		Resource("l2advertisements").
		Body(advertisement).
		Do(context.TODO()).
		Into(result)
	return result, err
}

func (c *MetallbClientSet) MakeL2Advertisement(name string, ipAddressPools []string) *metallbv1beta1.L2Advertisement {
	return &metallbv1beta1.L2Advertisement{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: metallbv1beta1.L2AdvertisementSpec{
			IPAddressPools: ipAddressPools,
		},
	}
}

func (c *MetallbClientSet) MakeIPAddressPool(name string, addresses []string, autoAssign bool) *metallbv1beta1.IPAddressPool {
	return &metallbv1beta1.IPAddressPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: metallbv1beta1.IPAddressPoolSpec{
			Addresses:  addresses,
			AutoAssign: &autoAssign,
		},
	}
}

func (c *MetallbClientSet) DeleteIPAddressPool(name string) error {
	return c.client.Delete().
		Namespace("metallb-system").
		Resource("ipaddresspools").
		Name(name).
		Do(context.TODO()).
		Error()
}

func (c *MetallbClientSet) DeleteL2Advertisement(name string) error {
	return c.client.Delete().
		Namespace("metallb-system").
		Resource("l2advertisements").
		Name(name).
		Do(context.TODO()).
		Error()
}

func (c *MetallbClientSet) ListServiceL2Statuses() (*metallbv1beta1.ServiceL2StatusList, error) {
	result := &metallbv1beta1.ServiceL2StatusList{}
	err := c.client.Get().
		Namespace("metallb-system").
		Resource("servicel2statuses").
		Do(context.TODO()).
		Into(result)
	return result, err
}
