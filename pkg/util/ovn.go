package util

import "strings"

const ProviderNetworkAddressPrefix = "pn"

func NodeLspName(node string) string {
	return NodeLspPrefix + node
}

func ProviderNetworkAddressName(providerNetwork, node string) string {
	const maxNameLength = 253
	name := strings.Join([]string{ProviderNetworkAddressPrefix, providerNetwork, node}, ".")
	if len(name) <= maxNameLength {
		return name
	}
	hash := Sha256Hash([]byte(name))[:16]
	return strings.TrimRight(name[:maxNameLength-len(hash)-1], ".-") + "." + hash
}
