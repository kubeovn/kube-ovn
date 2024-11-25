package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Sha256Hash(input []byte) string {
	hasher := sha256.New()
	hasher.Write(input)
	hashedBytes := hasher.Sum(nil)
	return hex.EncodeToString(hashedBytes)
}

func Sha256HashObject(obj any) (string, error) {
	buf, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	hasher.Write(buf)
	hashedBytes := hasher.Sum(nil)
	return hex.EncodeToString(hashedBytes), nil
}
