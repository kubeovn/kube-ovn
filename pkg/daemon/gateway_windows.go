package daemon

func (c *Controller) setIPSet() error {
	return nil
}

func (c *Controller) setPolicyRouting() error {
	return nil
}

func (c *Controller) setIptables() error {
	return nil
}

func (c *Controller) setGatewayBandwidth() error {
	// TODO
	return nil
}

func (c *Controller) setICGateway() error {
	// TODO
	return nil
}

func (c *Controller) setExGateway() error {
	// TODO
	return nil
}

//Generally, the MTU of the interface is set to 1400. But in special cases, a special pod (docker indocker) will introduce the docker0 interface to the pod. The MTU of docker0 is 1500.
//The network application in pod will calculate the TCP MSS according to the MTU of docker0, and then initiate communication with others. After the other party sends a response, the kernel protocol stack of Linux host will send ICMP unreachable message to the other party, indicating that IP fragmentation is needed, which is not supported by the other party, resulting in communication failure.
func (c *Controller) appendMssRule() {
}
