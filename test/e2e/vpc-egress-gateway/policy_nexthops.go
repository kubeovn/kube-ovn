package multus

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"k8s.io/utils/set"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var ipTokenRegexp = regexp.MustCompile(`[0-9A-Fa-f:.]+`)

func vpcEgressGatewayPolicyCount(veg *apiv1.VpcEgressGateway) int {
	if len(veg.Spec.Selectors) == 0 {
		return 1
	}
	return 2
}

func validateVpcEgressGatewayPolicyNexthops(output string, expected []string, expectedPolicyCount int) error {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != expectedPolicyCount || lines[0] == "" {
		return fmt.Errorf("got %d matching policies, expected %d", len(lines), expectedPolicyCount)
	}

	want := set.New(expected...)
	for _, line := range lines {
		got := set.New[string]()
		for _, token := range ipTokenRegexp.FindAllString(line, -1) {
			if net.ParseIP(token) != nil {
				got.Insert(token)
			}
		}
		if !want.Equal(got) {
			return fmt.Errorf("got policy nexthops %v, expected %v", got.UnsortedList(), expected)
		}
	}
	return nil
}
