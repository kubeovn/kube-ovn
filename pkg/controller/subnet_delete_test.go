package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestHandleDeleteSubnetBlockedByIP(t *testing.T) {
	now := metav1.NewTime(time.Now())
	tests := []struct {
		name string
		ip   *kubeovnv1.IP
	}{
		{
			name: "primary subnet",
			ip:   &kubeovnv1.IP{Name: "ip-a", Spec: kubeovnv1.IPSpec{Subnet: "subnet-a"}},
		},
		{
			name: "attach subnet",
			ip:   &kubeovnv1.IP{Name: "ip-a", Spec: kubeovnv1.IPSpec{Subnet: "other", AttachSubnets: []string{"subnet-a"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnet := &kubeovnv1.Subnet{
				Name:              "subnet-a",
				DeletionTimestamp: &now,
				Spec:              kubeovnv1.SubnetSpec{Vpc: "vpc-a"},
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{subnet},
				IPs:     []*kubeovnv1.IP{tt.ip},
			})
			require.NoError(t, err)

			require.ErrorContains(t, fc.fakeController.handleDeleteSubnet(subnet), "still has ip ip-a")
		})
	}
}
