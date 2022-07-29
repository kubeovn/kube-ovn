package daemon

func changeProvideNicName(nic, br string) (bool, error) {
	// not supported on windows
	return false, nil
}
