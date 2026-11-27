package main

import (
	"fmt"
	"os"

	"github.com/kubeovn/kube-ovn/pkg/nmhelper"
)

func main() {
	if err := nmhelper.ServeHelper(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
