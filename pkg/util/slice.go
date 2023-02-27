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

// UniqString creates an array of string with unique values.
func UniqString(a []string) []string {
	length := len(a)

	seen := make(map[string]struct{}, length)
	j := 0

	for i := 0; i < length; i++ {
		v := a[i]

		if _, ok := seen[v]; ok {
			continue
		}

		seen[v] = struct{}{}
		a[j] = v
		j++
	}

	return a[0:j]
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

func IsStringIn(str string, slice []string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// ContainsString Helper functions to check and remove string from a slice of strings.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
