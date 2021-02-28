package util

import v1 "k8s.io/api/core/v1"

func GetNodeInternalIP(node v1.Node) string {
	var nodeAddr string
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			nodeAddr = addr.Address
			break
		}
	}
	return nodeAddr
}
