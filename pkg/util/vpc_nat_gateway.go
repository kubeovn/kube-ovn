package util

import "fmt"

func GenNatGwStsName(name string) string {
	return fmt.Sprintf("vpc-nat-gw-%s", name)
}

func GenNatGwPodName(name string) string {
	return fmt.Sprintf("vpc-nat-gw-%s-0", name)
}
