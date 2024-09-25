package util

import "fmt"

// VpcNatGwNamePrefix is appended to the name of the StatefulSet and Pods for NAT gateways
var VpcNatGwNamePrefix = "vpc-nat-gw"

// GenNatGwStsName returns the full name of a NAT gateway StatefulSet
func GenNatGwStsName(name string) string {
	return fmt.Sprintf("%s-%s", VpcNatGwNamePrefix, name)
}

// GenNatGwPodName returns the full name of the NAT gateway pod within a StatefulSet
func GenNatGwPodName(name string) string {
	return fmt.Sprintf("%s-%s-0", VpcNatGwNamePrefix, name)
}
