// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package main

import (
	"fmt"
	"testing"

	"github.com/coronanet/go-coronanet/rest"
)

// Tests the basic operation of a pairing session.
func TestPairingLifecycle(t *testing.T) {
	// Create a pairing initiator and ensure both profile and networking is required
	alice, _ := newTestNode("", "--verbosity", "5", "--hostname", "alice")
	defer alice.close()

	if _, err := alice.InitPairing(); err == nil {
		t.Errorf("pairing initialized without profile")
	}
	alice.CreateProfile()
	alice.UpdateProfile(&rest.ProfileInfos{Name: "Alice"})

	if _, err := alice.InitPairing(); err == nil {
		t.Errorf("pairing initialized without networking")
	}
	// Enable networking too and ensure pairing can be started, once
	alice.EnableGateway()
	secret, err := alice.InitPairing()
	if err != nil {
		t.Fatalf("failed to initialize pairing: %v", err)
	}
	if _, err := alice.InitPairing(); err == nil {
		t.Errorf("duplicate pairing initialized")
	}
	// Create a pairing joiner and ensure profile and network requirements
	bob, _ := newTestNode("", "--verbosity", "5", "--hostname", "bob")
	defer bob.close()

	if _, err := bob.JoinPairing(secret); err == nil {
		t.Errorf("pairing joined without profile")
	}
	bob.CreateProfile()
	bob.UpdateProfile(&rest.ProfileInfos{Name: "Bob"})

	if _, err := bob.JoinPairing(secret); err == nil {
		t.Errorf("pairing joined without networking")
	}
	// Enable networking too and ensure pairing can be joined, once
	bob.EnableGateway()
	aliceFP, err := bob.JoinPairing(secret)
	if err != nil {
		t.Errorf("failed to join pairing: %v", err)
	}
	if _, err := bob.JoinPairing(secret); err == nil {
		t.Errorf("rejoine completed pairing session")
	}
	// Wait for the pairing initiator to complete too and cross check identities
	bobFP, err := alice.WaitPairing()
	if err != nil {
		t.Errorf("failed to wait for pairing: %v", err)
	}
	fmt.Println(aliceFP, bobFP)
}
