package v1

func (ss *SubnetStatus) ReleaseIP() {
	ss.UsingIPs--
	ss.AvailableIPs++
}

func (ss *SubnetStatus) AcquireIP() {
	ss.UsingIPs++
	ss.AvailableIPs--
}
