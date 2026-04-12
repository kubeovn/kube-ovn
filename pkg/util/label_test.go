package util

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestNormalizeLabelValue(t *testing.T) {
	t.Run("value within limit unchanged", func(t *testing.T) {
		value := strings.Repeat("a", validation.LabelValueMaxLength)
		if got := NormalizeLabelValue(value); got != value {
			t.Fatalf("expected unchanged label value, got %q", got)
		}
	})

	t.Run("overlong value normalized to max length deterministically", func(t *testing.T) {
		value := strings.Repeat("a", validation.LabelValueMaxLength+20)
		got := NormalizeLabelValue(value)

		if len(got) != validation.LabelValueMaxLength {
			t.Fatalf("expected label value length %d, got %d", validation.LabelValueMaxLength, len(got))
		}

		expectedHash := Sha256Hash([]byte(value))[:labelValueHashLength]
		if !strings.HasSuffix(got, "-"+expectedHash) {
			t.Fatalf("expected normalized value suffix -%s, got %q", expectedHash, got)
		}

		if got != NormalizeLabelValue(value) {
			t.Fatalf("expected deterministic normalization, got different outputs")
		}
	})
}
