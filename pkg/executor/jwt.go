package executor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// GenerateJWTToken generates a JWT token for Engine API authentication.
// The secret should be a hex-encoded string.
func GenerateJWTToken(secret string) (string, error) {
	// Decode the hex secret.
	secretBytes, err := hex.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decoding secret: %w", err)
	}

	// Create header.
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshaling header: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)

	// Create payload with iat (issued at) claim.
	payload := map[string]any{
		"iat": time.Now().Unix(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling payload: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	// Create message to sign.
	message := headerB64 + "." + payloadB64

	// Sign with HMAC-SHA256.
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return message + "." + signature, nil
}
