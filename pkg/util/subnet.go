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
