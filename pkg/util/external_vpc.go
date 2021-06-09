package util

type LogicalRouter struct {
	Name            string
	Ports           []Port
	LogicalSwitches []LogicalSwitch
}

type LogicalSwitch struct {
	Name  string
	Ports []Port
}

type Port struct {
	Name   string
	Subnet string
}
