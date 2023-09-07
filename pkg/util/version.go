package util

import (
	"strconv"
	"strings"
)

// CompareVersion compare two version
func CompareVersion(version1, version2 string) int {
	versionA := strings.Split(version1, ".")
	versionB := strings.Split(version2, ".")

	for i := len(versionA); i < 4; i++ {
		versionA = append(versionA, "0")
	}
	for i := len(versionB); i < 4; i++ {
		versionB = append(versionB, "0")
	}
	for i := 0; i < 4; i++ {
		version1, _ := strconv.Atoi(versionA[i])
		version2, _ := strconv.Atoi(versionB[i])

		switch {
		case version1 == version2:
			continue
		case version1 > version2:
			return 1
		default:
			return -1
		}
	}
	return 0
}
