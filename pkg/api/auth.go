package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const sessionTokenBytes = 32

// generateSessionToken creates a cryptographically random session token.
func generateSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}
