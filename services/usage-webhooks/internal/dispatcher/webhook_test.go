package dispatcher

import "testing"

func TestSign_IsDeterministic(t *testing.T) {
	body := []byte(`{"event_type":"operation.succeeded"}`)
	s1 := sign("shared-secret", body)
	s2 := sign("shared-secret", body)

	if s1 != s2 {
		t.Error("sign() should produce the same signature for the same secret+body")
	}
}

func TestSign_DifferentBodyProducesDifferentSignature(t *testing.T) {
	secret := "shared-secret"
	s1 := sign(secret, []byte(`{"a":1}`))
	s2 := sign(secret, []byte(`{"a":2}`))

	if s1 == s2 {
		t.Error("sign() produced identical signatures for different payloads — a customer verifying signatures would fail to detect tampering")
	}
}

// TestSign_DifferentSecretProducesDifferentSignature is what actually
// matters for webhook security: it proves the signature is keyed to the
// tenant's specific secret, so a signature valid for one customer's webhook
// cannot be replayed as valid for another's.
func TestSign_DifferentSecretProducesDifferentSignature(t *testing.T) {
	body := []byte(`{"event_type":"operation.succeeded"}`)
	s1 := sign("secret-one", body)
	s2 := sign("secret-two", body)

	if s1 == s2 {
		t.Error("sign() produced the same signature under two different secrets — this would let a signature from one tenant's webhook be replayed against another's")
	}
}
