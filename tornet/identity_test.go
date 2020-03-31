// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"bytes"
	"testing"
)

// Tests that a new random secret identity can be created.
func TestGenerateIdentity(t *testing.T) {
	if _, err := GenerateIdentity(); err != nil {
		t.Fatalf("Failed to generate new identity: %v", err)
	}
}

// Tests that the certificate for a random secret identity can be created, and
// also that it's deterministic.
func TestGenerateCertificate(t *testing.T) {
	id, _ := GenerateIdentity()
	if !bytes.Equal(id.certificate().Certificate[0], id.certificate().Certificate[0]) {
		t.Fatalf("Secret certificate not deterministic")
	}
}

// Tests that a new random secret address can be created.
func TestGenerateAddress(t *testing.T) {
	if _, err := GenerateAddress(); err != nil {
		t.Fatalf("Failed to generate new address: %v", err)
	}
}
