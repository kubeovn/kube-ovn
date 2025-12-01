package ovs

import "fmt"

const (
	ovsRunDir = "/var/run/openvswitch"
	ovnRunDir = "/var/run/ovn"

	cmdOvsAppctl = "ovs-appctl"

	OvsdbServer   = "ovsdb-server"
	OvsVswitchd   = "ovs-vswitchd"
	OvnController = "ovn-controller"
)

func Appctl(_ string, _ ...string) (string, error) {
	return "", fmt.Errorf("Appctl is not implemented on Windows")
}
