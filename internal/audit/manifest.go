package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func Digest(entries []string) string {
	h := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(h[:])
}
