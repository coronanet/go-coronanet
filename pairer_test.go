// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"bytes"
	"context"
	"testing"

	"github.com/coronanet/go-coronanet/tornet"
)

// Tests that basic pairing works.
func TestPairing(t *testing.T) {
	// Create two identities, one for initiating pairing and one for joining
	initKey, _ := tornet.GenerateIdentity()
	joinKey, _ := tornet.GenerateIdentity()

	// Initiate a pairing session and join it with the other identity
	gateway := tornet.NewMockGateway()

	initPairer, secret, err := newPairingServer(gateway, initKey.Public())
	if err != nil {
		t.Fatalf("failed to initiate pairing: %v", err)
	}
	joinPairer, err := newPairingClient(gateway, joinKey.Public(), secret)
	if err != nil {
		t.Fatalf("failed to join pairing: %v", err)
	}
	// Wait for both to finish
	joinPub, err := initPairer.wait(context.TODO())
	if err != nil {
		t.Fatalf("server side pairing failed: %v", err)
	}
	initPub, err := joinPairer.wait(context.TODO())
	if err != nil {
		t.Fatalf("client side pairing failed: %v", err)
	}
	// Ensure the exchanged secrets match
	initKeyBlob, _ := initKey.Public().MarshalJSON()
	initPubBlob, _ := initPub.MarshalJSON()
	if !bytes.Equal(initPubBlob, initKeyBlob) {
		t.Errorf("initer key mismatch: have %x, want %x", initPubBlob, initKeyBlob)
	}
	joinKeyBlob, _ := joinKey.Public().MarshalJSON()
	joinPubBlob, _ := joinPub.MarshalJSON()
	if !bytes.Equal(joinPubBlob, joinKeyBlob) {
		t.Errorf("joiner key mismatch: have %x, want %x", joinPubBlob, joinKeyBlob)
	}
}
