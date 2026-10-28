package ko

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

func validateResourceName(kind, value string) error {
	if problems := validation.IsDNS1123Subdomain(value); len(problems) != 0 {
		return fmt.Errorf("invalid %s name %q: %s", kind, value, strings.Join(problems, "; "))
	}
	return nil
}

func validatePodReference(value string) error {
	return validateNamespacedReference("pod", value)
}

func validateNamespacedReference(kind, value string) error {
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return fmt.Errorf("%s must be NAME or NAMESPACE/NAME, got %q", kind, value)
	}
	if len(parts) == 2 {
		if problems := validation.IsDNS1123Label(parts[0]); len(problems) != 0 {
			return fmt.Errorf("invalid %s namespace %q: %s", kind, parts[0], strings.Join(problems, "; "))
		}
	}
	return validateResourceName(kind, parts[len(parts)-1])
}
