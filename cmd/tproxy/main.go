package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kubeovn/kube-ovn/pkg/tproxy"
)

func main() {
	ovsSocket := flag.String("ovs-socket", "/run/openvswitch/db.sock", "Local OVSDB socket")
	flag.Parse()
	if err := tproxy.ServeNamespaceHelper(*ovsSocket); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
