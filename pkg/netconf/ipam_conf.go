package netconf

import "github.com/kubeovn/kube-ovn/pkg/request"

type IPAMConf struct {
	Type         string          `json:"type"`
	ServerSocket string          `json:"server_socket"`
	Provider     string          `json:"provider"`
	Routes       []request.Route `json:"routes"`
}
