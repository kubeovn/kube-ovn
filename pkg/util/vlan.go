package util

func IsNetworkVlan(networkType string) bool {
	return networkType == NetworkTypeHybrid || networkType == NetworkTypeVlan
}
