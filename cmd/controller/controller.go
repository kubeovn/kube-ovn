package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/controller"
	"fmt"
	"os"
)

func main() {
	config, err := controller.ParseFlags()
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(config)
	controller.RunServer(config)
}
