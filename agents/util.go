package agents

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
