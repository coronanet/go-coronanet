// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2019 Péter Szilágyi. All rights reserved.

// Package tornet is a P2P networking layer based on Tor connections.
//
// The goal of this overlay network is to allow anonymized, private communication
// between arbitrary entities that already know and trust each other. Establishing
// trust relationships is outside of scope here.
//
// An entity within the network (node, user, etc) is identified by a tuple:
//  - TLS certificate: Permanent, used for encryption and authentication
//  - Tor onion key:   Ephemeral, used for discovery and routing
//
// Why do we need two sets of keys? In theory, Tor provides all the necessary
// encryption through onion services. In practice, it's a general recommendation
// to add an extra application specific encryption layer on top to ensure that a
// compromise of the Tor network does not have catastrophic consequences on end-
// user privacy.
//
// Why do we use TLS for the application layer encryption? TLS has some very nice
// properties, namely: mutual encryption, mutual authentication (rarely used in a
// client-server setting) and battle testedness.
//
// The extra authentication layer on top from TLS provides an intriguing property,
// namely that onion keys can occasionally be discarded and regenerated. As long
// as only one party of a mutual trust regenerates its onion key, a connection
// between them can still be established (in one direction) and the new onion key
// safely exchanged. This is important, because anyone with access to the public
// onion address can do liveness checks on the peer, even if application layer
// authorization fails. By regenerating the onion address every time a peer is
// removed from the trusted set, the node instantly hides its liveness too from
// the removed peer.
//
// Other notable (both beneficial and detrimental) properties of constructing an
// overlay network on top of Tor is:
//  - Since the Tor network is the rendezvous point for all communication, all
//    issues regarding NATs and firewalls are solved (as long as Tor itself is
//    not blocked).
//  - Since Tor establishes 7 hop circuit between the connecting and listening
//    peers, both (as well as monitoring third parties) are oblivious to the
//    geographical and digital locations of the participants.
//  - Since all traffic passes through a significant number of Tor relays, the
//    network bandwidth available might fluctuate and in general may be of low
//    capacity. Nonetheless, it's generally enough for low-traffic-real-time or
//    high-traffic-background communication purposes (256kbps ballpark figure).
//
// API wise, the tornet package is kept to a minimum. Callers can instantiate a
// new instance of a network node with an initial set of trust relationships and
// later mutate this set. The created node will try to maintain live connections
// to all trusted peers and fire callbacks on success. Connections are deduped
// and throttled to avoid recourse exhaustion.
package tornet

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil"
	"github.com/cretz/bine/torutil/ed25519"
	"github.com/ethereum/go-ethereum/log"
)

var (
	// errSelfPeering is returned if the node is requested to peer with itself.
	errSelfPeering = errors.New("self peering")

	// errAlreadyPeering is returned if the node is requested to peer with someone
	// who's already on the list of trusted peers.
	errAlreadyPeering = errors.New("already peering")

	// errConflictingPeering is returned if the node is requested to peer with a
	// node that's already trusted under a different identity.
	errConflictingPeering = errors.New("conflicting peering")

	// errNotPeering is returns if the node is requested to untrust an unknown peer.
	errNotPeering = errors.New("not peering")
)

// Handler is a network callback for authenticated connections.
type Handler func(id string, conn net.Conn) error

// Node is a P2P participant of a decentralized overlay network fully deployed on
// top of the Tor network. A node can connect to arbitrarily many known/trusted
// peers, but will neither connect to, not accept inbound connections from non-
// trusted entities.
type Node struct {
	gateway Gateway // Gateway into the Tor network
	handler Handler // Network handler for successful connections

	owner *SecretIdentity            // Local identity for the onion service
	peers map[string]*PublicIdentity // Remote identities for the onion dials

	onion net.Listener             // Onion service for inbound connections
	dials map[string]chan struct{} // Notification channels to stop the dialers
	conns map[string]net.Conn      // Currently live connections

	quit chan chan error // Quit channel to gracefully stop the node
	pend sync.WaitGroup  // Pending connections before quit allowed
	lock sync.RWMutex    // Lock protecting the node's life cycle
}

// New creates a new tornet peer, seeding it with an identity and an initial set
// of trusted remote identities. The node needs to be provided with a Tor proxy
// as Tor currently does not support running multiple instances within the same
// process.
func New(gateway Gateway, owner *SecretIdentity, peers map[string]*PublicIdentity, handler Handler) *Node {
	// Create a copy of the peers (also ensures nil is fine)
	copy := make(map[string]*PublicIdentity)
	for id, peer := range peers {
		copy[id] = peer
	}
	return &Node{
		gateway: gateway,
		handler: handler,
		owner:   owner,
		peers:   copy,
	}
}

// Start opens the onion service and starts the peering processes both inbound
// and outbound.
func (node *Node) Start() error {
	// Ensure we don't start multiple instances
	node.lock.Lock()
	defer node.lock.Unlock()

	if node.quit != nil {
		return errors.New("already started")
	}
	// Start the Tor gateway and open the onion service
	config := &tor.ListenConf{
		Key:         node.owner.onion,
		RemotePorts: []int{1},
		Version3:    true,
	}
	onion, err := node.gateway.Listen(context.Background(), config)
	if err != nil {
		return err
	}
	// Start dialing outbound connections
	node.conns = make(map[string]net.Conn)
	node.dials = make(map[string]chan struct{})
	for id, peer := range node.peers {
		// Create a termination notification channel for this peer
		quit := make(chan struct{})
		node.dials[id] = quit

		// Start the persistent dialer for this peer
		node.pend.Add(1)
		go func(id string, peer *PublicIdentity) {
			defer node.pend.Done()
			node.dialer(id, peer, quit)
		}(id, peer)
	}
	// Start accepting inbound connections
	node.onion = onion
	node.quit = make(chan chan error)
	go node.server()

	return nil
}

// Stop requests the node to temporarily disconnect from the network. It may be
// restarted at a later point in time.
func (node *Node) Stop() error {
	// Ensure we don't stop terminated instances
	node.lock.Lock()
	if node.quit == nil {
		node.lock.Unlock()
		return errors.New("already stopped")
	}
	// Node running, terminate all network connections
	node.onion.Close()

	for id, notify := range node.dials {
		close(notify)
		if conn, ok := node.conns[id]; ok {
			conn.Close()
		}
	}
	node.dials = nil
	node.conns = nil
	node.lock.Unlock()

	// Wait for things to gracefully close down
	errc := make(chan error)
	node.quit <- errc
	err := <-errc

	// All network connections and listeners down, close and return
	node.quit = nil
	return err
}

// Trust adds a new public identity into the set of trusted peers. The node will
// immediately attempt to establish and maintain a connection.
func (node *Node) Trust(id string, peer *PublicIdentity) error {
	// Sanitize the identities (no self, no duplicate)
	node.lock.Lock()
	defer node.lock.Unlock()

	if bytes.Equal(node.owner.Public().owner, peer.owner) {
		return errSelfPeering
	}
	if _, ok := node.peers[id]; ok {
		return errAlreadyPeering
	}
	for _, peer := range node.peers {
		if bytes.Equal(peer.owner, peer.owner) {
			return errConflictingPeering
		}
	}
	// Peer legitimately new, add and start connecting
	node.peers[id] = peer

	quit := make(chan struct{})
	node.dials[id] = quit

	node.pend.Add(1)
	go func() {
		defer node.pend.Done()
		node.dialer(id, peer, quit)
	}()
	return nil
}

// Untrust removes a public identity from the set of trusted peers. The node will
// immediately break any active connections and forbid reconnects. Additionally,
// the node will reset its onion address to prevent the old trusted identity from
// mounting network DoS attacks.
func (node *Node) Untrust(id string) error {
	// Ensure the identity exists (i.e. don't reset onion pointlessly)
	node.lock.Lock()
	defer node.lock.Unlock()

	if _, ok := node.peers[id]; !ok {
		return errNotPeering
	}
	// Peer legitimately exists, remove trust and disconnect
	delete(node.peers, id)

	close(node.dials[id])
	delete(node.dials, id)

	if conn, ok := node.conns[id]; ok {
		conn.Close()
		delete(node.conns, id)
	}
	// To avoid DoS attacks from the dumped peer, change the onion address
	if err := node.owner.trash(); err != nil {
		return err
	}
	return nil
}

// server accepts inbound connections, runs the authentication handshake and
// starts the individual peer handler on success.
func (node *Node) server() {
	// Create the certificate pools for the local and remote servers
	localCerts := x509.NewCertPool()
	localCerts.AddCert(node.owner.cert())

	// Wrap the onion service into a TLS listener stream as we don't much trust
	// Tor to be the only encryption layer in the protocol.
	listener := tls.NewListener(node.onion, &tls.Config{
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			// Inbound connection, reconstruct all trusted certs on the fly
			node.lock.RLock()
			remoteCerts := x509.NewCertPool()
			for _, peer := range node.peers {
				remoteCerts.AddCert(peer.cert())
			}
			node.lock.RUnlock()

			return &tls.Config{
				Certificates: []tls.Certificate{node.owner.owner}, // We always authenticate with the owner
				RootCAs:      localCerts,                          // Local node is the server, that's the root CA
				ClientCAs:    remoteCerts,                         // Remote node is the client, that's the client CA
				ClientAuth:   tls.RequireAndVerifyClientCert,      // Force mutual TLS authentication
			}, nil
		},
	})
	// Start listening for remote connections and accept anything that managed to
	// get through Tor and pass the bidirectionally authenticated TLS handshake.
	var (
		errc chan error
		err  error
	)
	for errc == nil && err == nil {
		var conn net.Conn
		if conn, err = listener.Accept(); err == nil {
			log.Trace("Succeeded accepting peer")
			node.pend.Add(1)
			go func() {
				defer node.pend.Done()
				node.handle(conn)
			}()
		}
	}
	// Termination requested or something went wrong, clean up
	if errc == nil {
		errc = <-node.quit
	}
	errc <- err
}

// dialer keeps dialing one single peer, attempting to establish a connection.
// The connections are reattempted with an exponential backoff until the node
// is torn down.
func (node *Node) dialer(id string, peer *PublicIdentity, quit chan struct{}) {
	onion := torutil.OnionServiceIDFromPublicKey(peer.onion)
	logger := log.New("onion", onion)

	var (
		minDelay = 250 * time.Millisecond
		maxDelay = 5 * time.Minute
		delay    = minDelay
	)
	for {
		// If the node was stopped, otherwise wait for the backoff
		select {
		case <-quit:
			return
		case <-time.After(delay):
		}
		// If we already have a live connection, max out the delay
		node.lock.RLock()
		if node.conns == nil {
			// Node got terminated during connection, abort
			node.lock.RUnlock()
			return
		}
		_, connected := node.conns[id]
		node.lock.RUnlock()

		if connected {
			logger.Trace("Peer already connected")
			delay = maxDelay
			continue
		}
		// Connection can be attempted, don't double connect however
		logger.Trace("Attempting to dial peer")
		dialer, err := node.gateway.Dialer(context.TODO(), nil)
		if err != nil {
			logger.Trace("Failed to create Tor dialer", "err", err)
			if delay *= 2; delay > maxDelay {
				delay = maxDelay
			}
			continue
		}
		conn, err := dialer.Dial("tcp", fmt.Sprintf("%s.onion:1", onion))
		if err != nil {
			logger.Trace("Failed to dial peer", "err", err)
			if delay *= 2; delay > maxDelay {
				delay = maxDelay
			}
			continue
		}
		// Handle the connection until it's dropped
		logger.Trace("Succeeded dialing peer")
		delay = minDelay

		localCerts := x509.NewCertPool()
		localCerts.AddCert(node.owner.cert())

		remoteCerts := x509.NewCertPool()
		remoteCerts.AddCert(peer.cert())

		if err := node.handle(tls.Client(conn, &tls.Config{
			Certificates: []tls.Certificate{node.owner.owner}, // We always authenticate with the owner
			ServerName:   "localhost",                         // Everybody is localhost in the Tor network
			RootCAs:      remoteCerts,                         // Remote node is the server, that's the root CA
			ClientCAs:    localCerts,                          // Local node is the client, we are the client CA
		})); err != nil {
			logger.Trace("Failed to handle peer", "err", err)
			if delay *= 2; delay > maxDelay {
				delay = maxDelay
			}
			continue
		}
	}
}

// handle is responsible for doing the authentication handshake with a remote
// peer, and if passed, to establish a persistent data stream until it's torn
// down or breaks.
func (node *Node) handle(conn net.Conn) error {
	// Make sure the connection is torn down, whatever happens
	defer conn.Close()

	// Before doing anything, run the TLS handshake
	if err := conn.(*tls.Conn).Handshake(); err != nil {
		return err
	}
	// Retrieve the peer certificate and deduplicate connections
	cert := conn.(*tls.Conn).ConnectionState().PeerCertificates[0].Raw

	node.lock.Lock()
	if node.conns == nil {
		// Node got terminated during connection, abort
		node.lock.Unlock()
		return nil
	}
	// New connection, resolve the peer's public identity for the handshare
	var (
		id   string
		peer *PublicIdentity
	)
	for key, p := range node.peers {
		if bytes.Equal(cert, p.owner) {
			id, peer = key, p
			break
		}
	}
	if _, connected := node.conns[id]; connected {
		// Peer already connected, drop this attempt
		log.Trace("Peer already connected to us")
		node.lock.Unlock()
		return nil
	}
	node.conns[id] = conn
	node.lock.Unlock()

	// Ensure the connection is removed from the pool on disconnect
	defer func() {
		node.lock.Lock()
		defer node.lock.Unlock()

		delete(node.conns, id)
	}()
	// Before passing the connection up to the user handler, do a mini handshake to
	// exchange any potentially updated onion addresses due to trust revokations.
	go conn.Write(node.owner.onion.PublicKey())

	onion := make([]byte, len(peer.onion))
	if _, err := io.ReadFull(conn, onion); err != nil {
		return err
	}
	if !bytes.Equal(onion, peer.onion) {
		// Onion address changed, update locally
		log.Debug("Onion address changed",
			"old", torutil.OnionServiceIDFromPublicKey(peer.onion),
			"new", torutil.OnionServiceIDFromPublicKey(ed25519.PublicKey(onion)))

		node.lock.Lock()
		peer.onion = onion
		node.lock.Unlock()
	}
	// Handshake complete, pass the connection to the user
	return node.handler(id, conn)
}
