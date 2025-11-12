package util

// IgnoreCalicoPod returns true when the given annotations map contains the Calico pod IP key.
func IgnoreCalicoPod(annotations map[string]string) bool {
	if len(annotations) == 0 {
		return false
	}
	_, ok := annotations[CalicoPodIPAnnotation]
	return ok
}
