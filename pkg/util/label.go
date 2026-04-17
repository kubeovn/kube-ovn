package util

import (
	"fmt"

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
