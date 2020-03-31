// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Tests that secret key rings can be encoded into JSON format and parsed back.
func TestSecretKeyRingMarshalling(t *testing.T) {
	// Generate a local identity and assign a few remote identities to various
	// secret addresses
	var (
		id, _        = GenerateIdentity()
		addr1, _     = GenerateAddress()
		addr2, _     = GenerateAddress()
		peerId1, _   = GenerateIdentity()
		peerAddr1, _ = GenerateAddress()
		peerId2, _   = GenerateIdentity()
		peerAddr2, _ = GenerateAddress()
	)
	keyring := &SecretKeyRing{
		Identity:  id,
		Addresses: []SecretAddress{addr1, addr2},
		Trusted: map[IdentityFingerprint]RemoteKeyRing{
			peerId1.Fingerprint(): {Identity: peerId1.Public(), Address: peerAddr1.Public()},
			peerId1.Fingerprint(): {Identity: peerId2.Public(), Address: peerAddr2.Public()},
		},
		Accesses: map[AddressFingerprint]map[IdentityFingerprint]struct{}{
			addr1.Fingerprint(): {
				peerId1.Public().Fingerprint(): struct{}{},
				peerId2.Public().Fingerprint(): struct{}{},
			},
		},
	}
	original, _ := json.Marshal(keyring)

	keyring = new(SecretKeyRing)
	if err := json.Unmarshal(original, keyring); err != nil {
		t.Fatalf("Failed to parse encoded keyring: %v", err)
	}
	parsed, _ := json.Marshal(keyring)

	if !bytes.Equal(original, parsed) {
		t.Fatalf("Encode-parse-encode mismatch: have\n %s\n want\n %s", parsed, original)
	}
}
