// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"context"
	"net"
	"testing"
	"time"
)

// Tests that new remote identities can be injected into a peer set to accept new
// connections and they can also be removed to reject them.
func TestPeerSetTrustManagement(t *testing.T) {
	// Set up the crypto identities
	var (
		gateway       = NewMockGateway()
		serverId, _   = GenerateIdentity()
		serverAddr, _ = GenerateAddress()
		clientId, _   = GenerateIdentity()
	)
	// Create a server that does not trust the client
	serverPeers := NewPeerSet(PeerSetConfig{
		Handler: func(id IdentityFingerprint, conn net.Conn) {},
	})
	server, err := NewServer(ServerConfig{
		Gateway:  gateway,
		Address:  serverAddr,
		Identity: serverId,
		PeerSet:  serverPeers,
	})
	if err != nil {
		t.Fatalf("Failed to launch server: %v", err)
	}
	defer server.Close()

	// Ensure that connection to the server fails
	clientNotify := make(chan struct{}, 1)
	clientPeers := NewPeerSet(PeerSetConfig{
		Trusted: []PublicIdentity{serverId.Public()},
		Handler: func(id IdentityFingerprint, conn net.Conn) {
			clientNotify <- struct{}{}
		},
	})
	if err := DialServer(context.Background(), DialConfig{
		Gateway:  gateway,
		Address:  serverAddr.Public(),
		Server:   serverId.Public(),
		Identity: clientId,
		PeerSet:  clientPeers,
	}); err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	select {
	case <-clientNotify:
		t.Fatalf("Untrusted connection accepted")
	case <-time.After(100 * time.Millisecond):
		// Connection seem to have failed
	}
	// Inject the client into the server's trust ring and retry
	serverPeers.Trust(clientId.Public())

	if err := DialServer(context.Background(), DialConfig{
		Gateway:  gateway,
		Address:  serverAddr.Public(),
		Server:   serverId.Public(),
		Identity: clientId,
		PeerSet:  clientPeers,
	}); err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	select {
	case <-clientNotify:
		// Connection succeeded
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Connection timed out")
	}
	// Remove the client from the server's trust ring and retry
	serverPeers.Untrust(clientId.Fingerprint())

	if err := DialServer(context.Background(), DialConfig{
		Gateway:  gateway,
		Address:  serverAddr.Public(),
		Server:   serverId.Public(),
		Identity: clientId,
		PeerSet:  clientPeers,
	}); err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	select {
	case <-clientNotify:
		t.Fatalf("Untrusted connection accepted")
	case <-time.After(100 * time.Millisecond):
		// Connection seem to have failed
	}
}
