package otp

import (
	"crypto/sha256"
	"encoding/hex"
)

func Hash(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}
