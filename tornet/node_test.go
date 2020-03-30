// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"context"
	"net"
	"testing"
	"time"
)

// Test that nodes without remote peers don't choke on anything.
func TestNodeEmpty(t *testing.T) {
	keyring, err := GenerateKeyRing()
	if err != nil {
		t.Fatalf("Failed to generate keyring: %v", err)
	}
	node, err := NewNode(NodeConfig{
		Gateway: NewMockGateway(),
		KeyRing: keyring,
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()
}

// Tests that two nodes can connect to each other.
func TestNodeConnectivity(t *testing.T) {
	// Create the key rings for two users
	keyring1, _ := GenerateKeyRing()
	keyring2, _ := GenerateKeyRing()

	// Manually inject the cross trust into each other for this test
	keyring1.Trusted[keyring2.Identity.Fingerprint()] = RemoteKeyRing{
		Identity: keyring2.Identity.Public(),
		Address:  keyring2.Addresses[0].Public(),
	}
	keyring1.Accesses[keyring1.Addresses[0].Fingerprint()][keyring2.Identity.Fingerprint()] = struct{}{}

	keyring2.Trusted[keyring1.Identity.Fingerprint()] = RemoteKeyRing{
		Identity: keyring1.Identity.Public(),
		Address:  keyring1.Addresses[0].Public(),
	}
	keyring2.Accesses[keyring2.Addresses[0].Fingerprint()][keyring1.Identity.Fingerprint()] = struct{}{}

	// Create and boot the mutually trusting nodes
	gateway := NewMockGateway()

	notify1 := make(chan struct{}, 1)
	node1, _ := NewNode(NodeConfig{
		Gateway: gateway,
		KeyRing: keyring1,
		ConnHandler: func(id IdentityFingerprint, conn net.Conn) {
			notify1 <- struct{}{}
		},
	})
	defer node1.Close()

	notify2 := make(chan struct{}, 1)
	node2, _ := NewNode(NodeConfig{
		Gateway: gateway,
		KeyRing: keyring2,
		ConnHandler: func(id IdentityFingerprint, conn net.Conn) {
			notify2 <- struct{}{}
		},
	})
	defer node2.Close()

	// Connect the two nodes and wait for the pings
	if err := node1.Dial(context.Background(), keyring2.Identity.Fingerprint()); err != nil {
		t.Fatalf("Failed to dial peer: %v", err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-notify1:
			notify1 = nil
		case <-notify2:
			notify2 = nil
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Connection timed out")
		}
	}
}

// Tests that new remote identities can be injected into a node to accept new
// connections and they can also be removed to reject them.
func TestNodeTrustManagement(t *testing.T) {
	// Create the key rings for two users, have the second trust the first out of the box
	keyring1, _ := GenerateKeyRing()
	keyring2, _ := GenerateKeyRing()

	keyring2.Trusted[keyring1.Identity.Fingerprint()] = RemoteKeyRing{
		Identity: keyring1.Identity.Public(),
		Address:  keyring1.Addresses[0].Public(),
	}
	keyring2.Accesses[keyring2.Addresses[0].Fingerprint()][keyring1.Identity.Fingerprint()] = struct{}{}

	// To prevent the node from rotating the address on untrust, fake a second
	// peer. There's a different test that checks rotation.
	keyring1.Accesses[keyring1.Addresses[0].Fingerprint()]["fake peer"] = struct{}{}

	// Create and boot the first node, which does not trust the other
	gateway := NewMockGateway()

	node1, _ := NewNode(NodeConfig{
		Gateway:     gateway,
		KeyRing:     keyring1,
		RingHandler: func(keyring SecretKeyRing) {},
		ConnHandler: func(id IdentityFingerprint, conn net.Conn) {},
	})
	defer node1.Close()

	// Create and boot the second node, which does trust the other
	notify := make(chan struct{}, 1)
	node2, _ := NewNode(NodeConfig{
		Gateway: gateway,
		KeyRing: keyring2,
		ConnHandler: func(id IdentityFingerprint, conn net.Conn) {
			notify <- struct{}{}
		},
	})
	defer node2.Close()

	// Ensure that connection to the first node fails
	if err := node2.Dial(context.Background(), keyring1.Identity.Fingerprint()); err != nil {
		t.Fatalf("Failed to dial peer: %v", err)
	}
	select {
	case <-notify:
		t.Fatalf("Untrusted connection accepted")
	case <-time.After(100 * time.Millisecond):
		// Connection seem to have failed
	}
	// Inject the second identity into the first's trust ring and retry
	node1.Trust(RemoteKeyRing{
		Identity: keyring2.Identity.Public(),
		Address:  keyring2.Addresses[0].Public(),
	})
	if err := node2.Dial(context.Background(), keyring1.Identity.Fingerprint()); err != nil {
		t.Fatalf("Failed to dial peer: %v", err)
	}
	select {
	case <-notify:
		// Connection succeeded
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Connection timed out")
	}
	// Remove the client from the server's trust ring and retry
	node1.Untrust(keyring2.Identity.Fingerprint())

	if err := node2.Dial(context.Background(), keyring1.Identity.Fingerprint()); err != nil {
		t.Fatalf("Failed to dial peer: %v", err)
	}
	select {
	case <-notify:
		t.Fatalf("Untrusted connection accepted")
	case <-time.After(100 * time.Millisecond):
		// Connection seem to have failed
	}
}
