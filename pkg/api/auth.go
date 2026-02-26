package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	sessionTokenBytes = 32
	apiKeyBytes       = 32
	apiKeyPrefix      = "bmk_"
	apiKeyPrefixLen   = 8 // chars of hex portion to keep as display prefix
)

// generateSessionToken creates a cryptographically random session token.
func generateSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// generateAPIKey creates a new API key and returns the plaintext key,
// its SHA-256 hash (for storage), and a short prefix (for display).
func generateAPIKey() (plaintext, hash, prefix string, err error) {
	b := make([]byte, apiKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generating random bytes: %w", err)
	}

	hexPart := hex.EncodeToString(b)
	plaintext = apiKeyPrefix + hexPart

	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])

	prefix = hexPart[:apiKeyPrefixLen]

	return plaintext, hash, prefix, nil
}

// hashAPIKey returns the SHA-256 hex digest of a plaintext API key.
func hashAPIKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))

	return hex.EncodeToString(h[:])
}
