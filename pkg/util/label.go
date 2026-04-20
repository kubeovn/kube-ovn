package util

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	labelValueHashLength = 8
)

// NormalizeLabelValue returns a deterministic label value that always satisfies
// Kubernetes label value length limits. If the input already fits, it is returned unchanged.
func NormalizeLabelValue(value string) string {
	if len(value) <= validation.LabelValueMaxLength {
		return value
	}

	hash := Sha256Hash([]byte(value))[:labelValueHashLength]
	prefixLen := validation.LabelValueMaxLength - labelValueHashLength - 1
	return fmt.Sprintf("%s-%s", value[:prefixLen], hash)
}

// IPForLabel rewrites an IP address into a Kubernetes-label-value-safe form.
// IPv4 addresses already satisfy the `[a-zA-Z0-9._-]` rule and are returned
// as-is. IPv6 addresses contain `:`, which isn't a permitted character, so
// colons are replaced with `.` and any leading/trailing `.` (from `::`) is
// padded with `v6` so the result still starts and ends with alphanumerics.
// Empty input is passed through unchanged.
func IPForLabel(ip string) string {
	if ip == "" || !strings.Contains(ip, ":") {
		return ip
	}
	s := strings.ReplaceAll(ip, ":", ".")
	if strings.HasPrefix(s, ".") {
		s = "v6" + s
	}
	if strings.HasSuffix(s, ".") {
		s = s + "v6"
	}
	return s
}
