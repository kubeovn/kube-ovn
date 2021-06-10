package util

func IsNetworkVlan(networkType string) bool {
	return networkType == NetworkTypeHybrid || networkType == NetworkTypeVlan
}

// ExternalBridgeName returns external bridge name of the provider network
func ExternalBridgeName(provider string) string {
	return "br-" + provider
}
