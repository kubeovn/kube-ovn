package util

import (
	"strings"
)

func DoubleQuotedFields(s string) []string {
	var quoted bool
	var fields []string
	sb := &strings.Builder{}
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case !quoted && r == ' ':
			fields = append(fields, sb.String())
			sb.Reset()
		default:
			sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		fields = append(fields, sb.String())
	}

	return fields
}
