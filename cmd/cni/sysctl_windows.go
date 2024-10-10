package main

func sysctlEnableIPv6(nsPath string) error {
	// nothing to do on Windows
	return nil
}
