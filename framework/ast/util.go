package ast

import (
	"crypto/sha256"
	"fmt"
)

// GenerateFileID produces a stable identifier for a file path.
func GenerateFileID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("file:%x", sum[:8])
}

// HashContent returns a short hash for change detection.
func HashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}
