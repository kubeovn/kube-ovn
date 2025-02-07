package util

func DiffStringSlice(slice1, slice2 []string) []string {
	var diff []string

	// Loop two times, first to find slice1 strings not in slice2,
	// second loop to find slice2 strings not in slice1
	for i := 0; i < 2; i++ {
		for _, s1 := range slice1 {
			found := false
			for _, s2 := range slice2 {
				if s1 == s2 {
					found = true
					break
				}
			}
			// String not found. We add it to return slice
			if !found {
				diff = append(diff, s1)
			}
		}
		// Swap the slices, only if it was the first loop
		if i == 0 {
			slice1, slice2 = slice2, slice1
		}
	}
	return diff
}

func UnionStringSlice(slice1, slice2 []string) []string {
	if slice1 == nil {
		slice1 = []string{}
	}
	if slice2 == nil {
		slice2 = []string{}
	}

	uniqueMap := make(map[string]struct{})
	var union []string

	for _, s1 := range slice1 {
		uniqueMap[s1] = struct{}{}
	}
	for _, s2 := range slice2 {
		uniqueMap[s2] = struct{}{}
	}

	for key := range uniqueMap {
		union = append(union, key)
	}

	return union
}

// IsStringsOverlap check if two string slices are overlapped
func IsStringsOverlap(a, b []string) bool {
	for _, sa := range a {
		for _, sb := range b {
			if sa == sb {
				return true
			}
		}
	}
	return false
}

func RemoveString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return result
}
