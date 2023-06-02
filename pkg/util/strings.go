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
		if r == '"' {
			quoted = !quoted
		} else if !quoted && r == ' ' {
			fields = append(fields, sb.String())
			sb.Reset()
		} else {
			sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		fields = append(fields, sb.String())
	}

	return fields
}

func Sha256ByteToString(input []byte) string {
	hasher := sha256.New()
	hasher.Write(input)
	hashedBytes := hasher.Sum(nil)
	return hex.EncodeToString(hashedBytes)
}
