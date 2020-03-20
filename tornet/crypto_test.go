// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2019 Péter Szilágyi. All rights reserved.

package tornet

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Tests that a new random secret identity can be created.
func TestGenerateIdentity(t *testing.T) {
	if _, err := GenerateIdentity(); err != nil {
		t.Fatalf("Failed to generate new identity: %v", err)
	}
}

// Tests that secret identities can be encoded into JSON format and parsed back.
func TestSecretIdentityMarshalling(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("Failed to generate new identity: %v", err)
	}
	original, _ := json.Marshal(id)

	id = new(SecretIdentity)
	if err = json.Unmarshal(original, id); err != nil {
		t.Fatalf("Failed to parse encoded identity: %v", err)
	}
	parsed, _ := json.Marshal(id)

	if !bytes.Equal(original, parsed) {
		t.Fatalf("Encode-parse-encode mismatch: have\n %s\n want\n %s", parsed, original)
	}
}

// Tests that public identities can be encoded into JSON format and parsed back.
func TestPublicIdentityMarshalling(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("Failed to generate new identity: %v", err)
	}
	pub := id.Public()
	original, _ := json.Marshal(pub)

	pub = new(PublicIdentity)
	if err = json.Unmarshal(original, pub); err != nil {
		t.Fatalf("Failed to parse encoded identity: %v", err)
	}
	parsed, _ := json.Marshal(pub)

	if !bytes.Equal(original, parsed) {
		t.Fatalf("Encode-parse-encode mismatch: have\n %s\n want\n %s", parsed, original)
	}
}

// Benchmarks the speed of generating new identities.
func BenchmarkGenerateIdentity(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateIdentity()
	}
}
