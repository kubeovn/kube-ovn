package ovs

import "errors"

func Appctl(_ string, _ ...string) (string, error) {
	return "", errors.New("ovs-appctl is not implemented on Windows")
}
