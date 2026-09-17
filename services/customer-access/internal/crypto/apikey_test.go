package crypto

import "testing"

func TestGenerateRawKey_HasExpectedPrefix(t *testing.T) {
	raw, prefix, err := GenerateRawKey()
	if err != nil {
		t.Fatalf("GenerateRawKey returned error: %v", err)
	}
	if len(raw) < len("nx_live_") {
		t.Fatalf("generated key too short: %q", raw)
	}
	if raw[:8] != "nx_live_" {
		t.Errorf("expected key to start with nx_live_, got %q", raw)
	}
	if prefix != raw[:16] {
		t.Errorf("expected prefix to be first 16 chars of raw key; prefix=%q raw=%q", prefix, raw)
	}
}

func TestGenerateRawKey_ProducesUniqueKeys(t *testing.T) {
	seen := make(map[string]bool)
	const n = 200
	for i := 0; i < n; i++ {
		raw, _, err := GenerateRawKey()
		if err != nil {
			t.Fatalf("GenerateRawKey returned error: %v", err)
		}
		if seen[raw] {
			t.Fatalf("GenerateRawKey produced a duplicate key on iteration %d — entropy source may be broken", i)
		}
		seen[raw] = true
	}
}

func TestHashKey_IsDeterministic(t *testing.T) {
	secret := "test-secret"
	raw := "nx_live_abc123"

	h1 := HashKey(secret, raw)
	h2 := HashKey(secret, raw)

	if h1 != h2 {
		t.Errorf("HashKey should be deterministic for the same secret+key; got %q and %q", h1, h2)
	}
}

func TestHashKey_DifferentKeysProduceDifferentHashes(t *testing.T) {
	secret := "test-secret"
	h1 := HashKey(secret, "nx_live_aaaaaaaa")
	h2 := HashKey(secret, "nx_live_bbbbbbbb")

	if h1 == h2 {
		t.Error("HashKey produced identical hashes for two different raw keys")
	}
}

// This is the test that actually matters for the security story: it proves
// the hash is keyed (HMAC), not a plain digest. If an attacker somehow
// gained read access to the database (key_hash column) but NOT the
// HMAC secret, they must not be able to verify guesses offline — which
// requires that the same raw key hashes differently under a different
// secret.
func TestHashKey_IsSecretDependent(t *testing.T) {
	raw := "nx_live_same_key_both_times"
	h1 := HashKey("secret-one", raw)
	h2 := HashKey("secret-two", raw)

	if h1 == h2 {
		t.Error("HashKey produced the same output for two different secrets — this would mean the hash is not actually keyed, defeating the purpose of using HMAC over a plain digest")
	}
}
