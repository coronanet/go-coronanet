// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"context"
	"net"
	"testing"
	"time"
)

// Tests that a client and a server can connect to each other and mutually
// complete the TLS handshakes.
func TestServerConnectivity(t *testing.T) {
	// Set up the crypto identities and trusts
	var (
		gateway       = NewMockGateway()
		serverId, _   = GenerateIdentity()
		serverAddr, _ = GenerateAddress()
		clientId, _   = GenerateIdentity()
	)
	// Create a server that accepts a single client and signals on a channel
	serverNotify := make(chan struct{}, 1)
	serverPeers := NewPeerSet(PeerSetConfig{
		Trusted: []PublicIdentity{clientId.Public()},
		Handler: func(id IdentityFingerprint, conn net.Conn) {
			serverNotify <- struct{}{}
		},
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

	// Create a client that connects to the server and signals on a channel
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
	// Wait for both server and client to notify and return
	for i := 0; i < 2; i++ {
		select {
		case <-serverNotify:
			serverNotify = nil
		case <-clientNotify:
			clientNotify = nil
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Connection timed out")
		}
	}
}
