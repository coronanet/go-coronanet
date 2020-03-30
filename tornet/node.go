// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

// NodeConfig can be used to fine tune the initial setup of a tornet node.
type NodeConfig struct {
	Gateway     Gateway       // Tor gateway to network through
	KeyRing     SecretKeyRing // Key ring for peer management
	RingHandler RingHandler   // Handler to run for keyring changes
	ConnHandler ConnHandler   // Handler to run for each peer
}

// RingHandler is a callback for local or remote keyring changes.
type RingHandler func(keyring SecretKeyRing)

// Node is a network entity of a decentralized overlay network fully deployed on
// top of the Tor network. It acts as a peer-to-peer node, listening at the same
// time on multiple addresses and maintaining deduplicated connections to many
// remote peers.
//
// The node is however more than a collection of tornet servers and connections.
// It also incorporates a Tor address rotation mechanism. Every time a contact is
// removed from the trust ring, a new tornet server is launched with the aim of
// moving everyone over eventually. At that point the old address can be removed,
//
type Node struct {
	gateway Gateway       // Tor gateway to network through
	keyring SecretKeyRing // Cryptographic credentials to connect with and manage
	peerset *PeerSet      // Peer handler for successfully established connections

	ringHandler RingHandler // System handler to run after keyring updates
	connHandler ConnHandler // Application handler to run after address exchange

	servers []*Server    // Remote connection listeners in the Tor network
	lock    sync.RWMutex // Ensures the internals are not modified concurrently
}

// NewNode creates a new tornet P2P node which can initiate and accept remote
// connections over Tor.
func NewNode(config NodeConfig) (*Node, error) {
	// Create a blank to to allow setting callbacks
	node := &Node{
		gateway:     config.Gateway,
		keyring:     config.KeyRing,
		ringHandler: config.RingHandler,
		connHandler: config.ConnHandler,
	}
	// Create the peer set to deduplicate and handle connections
	trusted := make([]PublicIdentity, 0, len(node.keyring.Trusted))
	for _, trust := range node.keyring.Trusted {
		trusted = append(trusted, trust.Identity)
	}
	node.peerset = NewPeerSet(PeerSetConfig{
		Trusted: trusted,
		Handler: node.handle,
	})
	// For every currently maintained address, launch a listener server
	for _, address := range node.keyring.Addresses {
		server, err := NewServer(ServerConfig{
			Gateway:  node.gateway,
			Address:  address,
			Identity: node.keyring.Identity,
			PeerSet:  node.peerset,
		})
		if err != nil {
			// If something failed, tear down any already created servers
			for _, server := range node.servers {
				server.Close()
			}
			return nil, err
		}
		node.servers = append(node.servers, server)
	}
	return node, nil
}

// Close terminates all the network listeners and tears down all connections.
func (n *Node) Close() error {
	// Terminate all servers first to ensure no more peers get in
	n.lock.RLock()
	for _, server := range n.servers {
		server.Close()
	}
	n.lock.RUnlock()

	// Terminate the peer set to ensure all active connections are torn down
	n.peerset.Close()

	// All network connections torn down, gut the internals
	n.servers = nil
	return nil
}

// Dial requests the node to connect to an already configured remote peer.
func (n *Node) Dial(ctx context.Context, id IdentityFingerprint) error {
	// Retrieve the keyring of the requested peer and fail if unknown
	n.lock.RLock()
	keyring, ok := n.keyring.Trusted[id]
	if !ok {
		n.lock.RUnlock()
		return errors.New("unknown identity")
	}
	n.lock.RUnlock()

	// Address located, attempt to dial it
	return DialServer(ctx, DialConfig{
		Gateway:  n.gateway,
		Address:  keyring.Address,
		Server:   keyring.Identity,
		Identity: n.keyring.Identity,
		PeerSet:  n.peerset,
	})
}

// handle is responsible for doing a cryptographic address exchange between two
// mutually trusted peers for server rotation. Afterwards, the connection will
// be passed up to any application handler.
func (n *Node) handle(id IdentityFingerprint, conn net.Conn) {
	logger := log.New("peer", id)

	// Connection has been mutually authenticated at the TLS level. Send over
	// the address the local server prefers to be contacted on and the address
	// the local server believes the remote connection prefers to be contacted
	// on. Also read the other side's preferences.
	n.lock.RLock()
	preferredLocalAddress := n.keyring.Addresses[len(n.keyring.Addresses)-1].Public()
	believedRemoteAddress := n.keyring.Trusted[id].Address
	n.lock.RUnlock()

	conn.SetDeadline(time.Now().Add(time.Second))

	var (
		errc                   = make(chan error, 2)
		requestedRemoteAddress = make(PublicAddress, len(believedRemoteAddress))
		believedLocalAddress   = make(PublicAddress, len(preferredLocalAddress))
	)
	go func() {
		if _, err := conn.Write(preferredLocalAddress); err != nil {
			errc <- err
			return
		}
		_, err := conn.Write(believedRemoteAddress)
		errc <- err
	}()
	go func() {
		if _, err := conn.Read(requestedRemoteAddress); err != nil {
			errc <- err
			return
		}
		_, err := conn.Read(believedLocalAddress)
		errc <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			logger.Warn("Address exchange failed", "err", err)
			return
		}
	}
	conn.SetDeadline(time.Time{})

	// If the remote peer updated its preferred address, update locally too. This
	// ensures that next time we will connect to the correct address, even if the
	// current connection is a bit misguided.
	if !bytes.Equal(believedRemoteAddress, requestedRemoteAddress) {
		n.handleNewAddress(id, requestedRemoteAddress)
	}
	// If the remote peer believes in the same local address that we want them to
	// believe in, move them to that address pool (if not already there). This
	// ensures that old addresses can be rotated out and terminated.
	if bytes.Equal(preferredLocalAddress, believedLocalAddress) {
		n.handleMaybeNewAccess(id, preferredLocalAddress.Fingerprint())
	}
	logger.Debug("Rotating addresses exchanged", "local", preferredLocalAddress.Fingerprint(), "remote", requestedRemoteAddress.Fingerprint())

	// All exchanges successful, let the application layer take over
	n.connHandler(id, conn)
}

// handleNewAddress handles the remote announcement of a new tornet address.
func (n *Node) handleNewAddress(id IdentityFingerprint, addr PublicAddress) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.keyring.Trusted[id] = RemoteKeyRing{
		Identity: n.keyring.Trusted[id].Identity,
		Address:  addr,
	}
	n.ringHandler(n.keyring)
}

// handleMaybeNewAccess handles the remote acknowledgement of a new tornet address.
func (n *Node) handleMaybeNewAccess(peerId IdentityFingerprint, addrId AddressFingerprint) {
	n.lock.Lock()
	defer n.lock.Unlock()

	// If the peer is already in the correct pool, leave as is
	if _, ok := n.keyring.Accesses[addrId][peerId]; ok {
		return
	}
	// Nope, drop the peer from it's current pool and move into the target
	for addr, peers := range n.keyring.Accesses {
		if _, ok := peers[peerId]; ok {
			delete(peers, peerId)
			if len(peers) == 0 {
				n.dropServer(addr)
			}
			break
		}
	}
	n.keyring.Accesses[addrId][peerId] = struct{}{}
	n.ringHandler(n.keyring)
}

// dropServer removes a dud server and it's address from the keyring.
//
// This methods assumes the write lock is held.
func (n *Node) dropServer(uid AddressFingerprint) {
	// Remove any address-to-identity access mappings
	delete(n.keyring.Accesses, uid)

	// Find the dud server index, remove its address and server
	for i, addr := range n.keyring.Addresses {
		if addr.Fingerprint() == uid {
			n.keyring.Addresses = append(n.keyring.Addresses[:i], n.keyring.Addresses[i+1:]...)

			n.servers[i].Close()
			n.servers = append(n.servers[:i], n.servers[i+1:]...)
		}
	}
	n.ringHandler(n.keyring)
}

// Trust adds a new remote keyring into the node's internal ring.
func (n *Node) Trust(keyring RemoteKeyRing) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	// Inject the identity in the peer set to allow inbound connections
	if err := n.peerset.Trust(keyring.Identity); err != nil {
		return err
	}
	// Inject the identity into the keyring too
	uid := keyring.Identity.Fingerprint()
	if _, ok := n.keyring.Trusted[uid]; ok {
		// This is just a sanity panic if we mess something up in the implementation
		panic(fmt.Sprintf("peer known in keyring/trusted but not in peerset"))
	}
	n.keyring.Trusted[uid] = keyring

	addr := n.keyring.Addresses[len(n.keyring.Addresses)-1].Fingerprint()
	if _, ok := n.keyring.Accesses[addr][uid]; ok {
		// This is just a sanity panic if we mess something up in the implementation
		panic(fmt.Sprintf("peer known in keyring/accesses but not in peerset"))
	}
	n.keyring.Accesses[addr][uid] = struct{}{}
	n.ringHandler(n.keyring)
	return nil
}

// Untrust removes a remote keyring from the node's internal ring. Connections
// matching the untrusted identity will also be dropped.
func (n *Node) Untrust(uid IdentityFingerprint) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	// Remove the identity from the peer set to drop live connections
	if err := n.peerset.Untrust(uid); err != nil {
		return err
	}
	// Remove the identity from the keyring too
	if _, ok := n.keyring.Trusted[uid]; !ok {
		// This is just a sanity panic if we mess something up in the implementation
		panic(fmt.Sprintf("peer known in peerset but not in keyring/trusted"))
	}
	delete(n.keyring.Trusted, uid)

	for addr, peers := range n.keyring.Accesses {
		if _, ok := peers[uid]; ok {
			delete(peers, uid)
			if len(peers) == 0 {
				n.dropServer(addr)
			}
			break
		}
	}
	n.ringHandler(n.keyring)
	return nil
}
