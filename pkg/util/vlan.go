package util

// ExternalBridgeName returns external bridge name of the provider network
func ExternalBridgeName(provider string) string {
	return "br-" + provider
}
