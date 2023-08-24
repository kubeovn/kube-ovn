package util

import (
	"crypto/sha256"
	"encoding/hex"
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

func Sha256Hash(input []byte) string {
	hasher := sha256.New()
	hasher.Write(input)
	hashedBytes := hasher.Sum(nil)
	return hex.EncodeToString(hashedBytes)
}
