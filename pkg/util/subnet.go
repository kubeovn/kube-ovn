package util

import "strings"

func IsOvnProvider(provider string) bool {
	if provider == "" || provider == OvnProvider {
		return true
	}
	if fields := strings.Split(provider, "."); len(fields) == 3 && fields[2] == OvnProvider {
		return true
	}
	return false
}

func GetNadBySubnetProvider(provider string) (nadName, nadNamespace string, existNad bool) {
	fields := strings.Split(provider, ".")
	switch {
	case len(fields) == 3 && fields[2] == OvnProvider:
		return fields[0], fields[1], true
	case len(fields) == 2:
		return fields[0], fields[1], true
	}
	return "", "", false
}
