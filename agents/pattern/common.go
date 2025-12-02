package pattern

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewUUID returns a hex string suitable for correlating memory artifacts. It
// falls back to a timestamp when crypto/rand is unavailable.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
