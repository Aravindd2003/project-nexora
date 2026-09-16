package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateRawKey creates a new high-entropy API key with a recognizable
// prefix (nx_live_) so leaked keys are identifiable in logs/scanners.
func GenerateRawKey() (raw string, prefix string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate key entropy: %w", err)
	}
	raw = "nx_live_" + hex.EncodeToString(buf)
	prefix = raw[:16] // shown in dashboards; not sensitive on its own
	return raw, prefix, nil
}

// HashKey applies a keyed HMAC-SHA256 over the raw API key.
//
// We use a keyed HMAC (not a plain SHA-256 digest, and not a slow password
// hash like argon2id) because API keys are already high-entropy random
// tokens rather than user-chosen passwords: there is no offline dictionary
// attack to slow down, so the priority is a fast, constant-time-comparable
// digest that only Nexora (holder of the HMAC secret) can compute — which
// prevents an attacker with DB read access alone from forging valid keys.
func HashKey(secret, raw string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}
