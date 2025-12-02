package ovs

import "errors"

const (
	OvsdbServer   = "ovsdb-server"
	OvsVswitchd   = "ovs-vswitchd"
	OvnController = "ovn-controller"
)

func Appctl(_ string, _ ...string) (string, error) {
	return "", errors.New("ovs-appctl is not implemented on Windows")
}
