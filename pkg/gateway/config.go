package gateway

import (
	"flag"
	"github.com/spf13/pflag"
)

type Configuration struct {
	Interface         string
	SnatIP            string
	ClusterRouterIP   string
	EdgeRouterName    string
	EdgeRouterIP      string
	Chassis           string
	TransitSwitchName string
	OutsideSwitchName string
	ClusterRouterName string
	OvnNbHost         string
	OvnNbPort         int
	OvnSbHost         string
	OvnSbPort         int
}

func ParseFlags() (*Configuration, error) {
	var (
		argInterface         = pflag.String("interface", "eth0", "The gateway interface")
		argSnatIP            = pflag.String("snat-ip", "", "The snat ip")
		argClusterRouterIP   = pflag.String("cluster-router-ip", "172.16.255.2/30", "The cluster route to transit switch ip")
		argEdgeRouterIP      = pflag.String("edge-router-ip", "172.16.255.1/30", "The edge router to transit switch ip")
		argChassis           = pflag.String("chassis", "", "chassis id found in ovn-sb")
		argTransitSwitchName = pflag.String("transit-switch-name", "transit", "The name of switch between cluster router and edge router.")
		argOutsideSwitchName = pflag.String("outside-switch-name", "outside", "The name of switch between edge router and physic interface.")
		argEdgeRouterName    = pflag.String("edge-router-name", "edge", "The edge router name")
		argClusterRouterName = pflag.String("cluster-router-name", "ovn-cluster", "The cluster router name")
		argOvnNbHost         = pflag.String("ovn-nb-host", "", "")
		argOvnNbPort         = pflag.Int("ovn-nb-port", 6641, "")
		argOvnSbHost         = pflag.String("ovn-sb-host", "", "")
		argOvnSbPort         = pflag.Int("ovn-sb-port", 6642, "")
	)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	flag.CommandLine.Parse(make([]string, 0))

	config := &Configuration{
		Interface:         *argInterface,
		SnatIP:            *argSnatIP,
		ClusterRouterIP:   *argClusterRouterIP,
		EdgeRouterIP:      *argEdgeRouterIP,
		EdgeRouterName:    *argEdgeRouterName,
		Chassis:           *argChassis,
		TransitSwitchName: *argTransitSwitchName,
		ClusterRouterName: *argClusterRouterName,
		OutsideSwitchName: *argOutsideSwitchName,
		OvnNbHost:         *argOvnNbHost,
		OvnNbPort:         *argOvnNbPort,
		OvnSbHost:         *argOvnSbHost,
		OvnSbPort:         *argOvnSbPort,
	}

	return config, nil
}
