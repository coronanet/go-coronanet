// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package main

import (
	"testing"

	"github.com/coronanet/go-coronanet/rest"
)

// Tests the basic operation of a pairing session.
func TestPairingLifecycle(t *testing.T) {
	// Create a pairing initiator and ensure both profile and networking is required
	alice, _ := newTestNode("", "--verbosity", "5", "--hostname", "alice")
	defer alice.close()

	if _, err := alice.InitPairing(); err == nil {
		t.Fatalf("pairing initialized without profile")
	}
	alice.CreateProfile()
	alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"})

	if _, err := alice.InitPairing(); err == nil {
		t.Fatalf("pairing initialized without networking")
	}
	// Enable networking too and ensure pairing can be started, once
	alice.EnableGateway()
	secret, err := alice.InitPairing()
	if err != nil {
		t.Fatalf("failed to initialize pairing: %v", err)
	}
	if _, err := alice.InitPairing(); err == nil {
		t.Fatalf("duplicate pairing initialized")
	}
	// Create a pairing joiner and ensure profile and network requirements
	bob, _ := newTestNode("", "--verbosity", "5", "--hostname", "bobby")
	defer bob.close()

	if _, err := bob.JoinPairing(secret); err == nil {
		t.Fatalf("pairing joined without profile")
	}
	bob.CreateProfile()
	bob.UpdateProfile(&rest.ProfileInfos{Name: "Bob"})

	if _, err := bob.JoinPairing(secret); err == nil {
		t.Fatalf("pairing joined without networking")
	}
	// Enable networking too and ensure pairing can be joined, once
	bob.EnableGateway()
	if _, err := bob.JoinPairing(secret); err != nil {
		t.Fatalf("failed to join pairing: %v", err)
	}
	if _, err := bob.JoinPairing(secret); err == nil {
		t.Fatalf("managed to join finished pairing")
	}
	// Wait for the pairing initiator to complete too
	if _, err := alice.WaitPairing(); err != nil {
		t.Fatalf("failed to wait for pairing: %v", err)
	}
	if _, err := alice.WaitPairing(); err == nil {
		t.Fatalf("manged to wait on finished pairing")
	}
}
