// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

// protocolMagic is a small set of initial bytes that are exchanged across an
// established tornet link. Its purpose is to force all TLS negotiations to
// finish and check that the encrypted streams are properly set up and accepted
// in both directions.
const protocolMagic = "COVID-19"

// ConnHandler is a network callback for authenticated connections.
type ConnHandler func(id IdentityFingerprint, conn net.Conn, logger log.Logger)

// PeerSetConfig can be used to fine tune the initial setup of a tornet peerset.
type PeerSetConfig struct {
	Trusted []PublicIdentity // Initial set of trusted authorizations
	Handler ConnHandler      // Handler to run for each added connection
	Timeout time.Duration    // Maximum idle time after which to disconnect

	Logger log.Logger // Logger to allow injecting pre-networking context
}

// PeerSet is a collection of live network connections through Tor. It's purpose
// is to allow de-duplicating connections that might arrive from a variety of
// onion addresses.
type PeerSet struct {
	gateway Gateway       // Tor gateway to open the listener through
	handler ConnHandler   // Network to run for each added connection
	timeout time.Duration // Maximum idle time after which to disconnect

	auths map[IdentityFingerprint]PublicIdentity // Remote identities for inbound dials
	conns map[IdentityFingerprint]net.Conn       // Currently live remote connections

	logger log.Logger   // Contextual logger with optional embedded tags
	lock   sync.RWMutex // Lock protecting the set's internals
}

// NewPeerSet create an empty peer set, pre-authorized with a set of cryptographic
// remote identities.
func NewPeerSet(config PeerSetConfig) *PeerSet {
	peerset := &PeerSet{
		handler: config.Handler,
		timeout: config.Timeout,
		auths:   make(map[IdentityFingerprint]PublicIdentity),
		conns:   make(map[IdentityFingerprint]net.Conn),
		logger:  config.Logger,
	}
	for _, auth := range config.Trusted {
		peerset.auths[auth.Fingerprint()] = auth
	}
	if peerset.logger == nil {
		peerset.logger = log.Root()
	}
	return peerset
}

// Close terminates all peer connections.
func (ps *PeerSet) Close() error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if ps.conns == nil {
		return nil
	}
	for _, conn := range ps.conns {
		conn.Close()
	}
	ps.conns = nil
	return nil
}

// handle is responsible for doing the authentication handshake with a remote
// peer, and if passed, to establish a persistent data stream until it's torn
// down or breaks.
func (ps *PeerSet) handle(conn net.Conn, done chan error) {
	// Make sure the connection is torn down, whatever happens
	defer conn.Close()

	// Before doing anything, run the TLS handshake
	if err := conn.(*tls.Conn).Handshake(); err != nil {
		ps.logger.Warn("Remote connection failed authentication", "err", err)
		done <- err
		return
	}
	// Retrieve the peer certificate and deduplicate connections
	pub := conn.(*tls.Conn).ConnectionState().PeerCertificates[0].PublicKey
	uid := PublicIdentity(pub.(ed25519.PublicKey)).Fingerprint()

	logger := ps.logger.New("peer", uid)

	ps.lock.Lock()
	if _, ok := ps.auths[uid]; !ok {
		// This path triggers if the server permitted a peer to connect to us,
		// but that peer was not authorized to do so. It signals a bad usage
		// of the package.
		logger.Error("Connection accepted but peer not trusted")
		ps.lock.Unlock()
		done <- errors.New("untrusted connection")
		return
	}
	if _, ok := ps.conns[uid]; ok {
		logger.Debug("New peer connection deduplicated")
		ps.lock.Unlock()
		done <- errors.New("duplicate connection")
		return
	}
	logger.Debug("New peer connection established")
	ps.conns[uid] = conn
	ps.lock.Unlock()

	// Ensure the connection is removed from the pool on disconnect
	defer func() {
		ps.lock.Lock()
		defer ps.lock.Unlock()

		logger.Debug("Peer connection torn down")
		delete(ps.conns, uid)
	}()
	// TLS seems to be ok, at least on this side. To ensure it's ok in both of
	// the directions, exchange the initial protocol magic.
	conn.SetDeadline(time.Now().Add(time.Second))

	var (
		errc = make(chan error, 2)
		helo = make([]byte, len(protocolMagic))
	)
	go func() {
		_, err := conn.Write([]byte(protocolMagic))
		errc <- err
	}()
	go func() {
		_, err := conn.Read(helo)
		errc <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			logger.Warn("Protocol validation failed", "err", err)
			done <- err
			return
		}
	}
	if string(helo) != protocolMagic {
		logger.Warn("Protocol magic mismatch", "magic", helo)
		done <- errors.New("magic mismatch")
		return
	}
	conn.SetDeadline(time.Time{})

	// Handshake complete, initiate the time breaker and pass to the user
	if ps.timeout != 0 {
		conn = newBreaker(conn, ps.timeout)
	}
	ps.handler(uid, conn, ps.logger)
	done <- nil
}

// Trust adds a new public identity into the set of trusted peers.
func (ps *PeerSet) Trust(id PublicIdentity) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	uid := id.Fingerprint()
	if _, ok := ps.auths[uid]; ok {
		return errors.New("already trusted")
	}
	ps.auths[uid] = id
	return nil
}

// Untrust removes a public identity from the set of trusted peers. Connections
// matching the untrusted identity will also be dropped.
func (ps *PeerSet) Untrust(uid IdentityFingerprint) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if _, ok := ps.auths[uid]; !ok {
		return errors.New("not trusted")
	}
	if conn, ok := ps.conns[uid]; ok {
		conn.Close()
	}
	delete(ps.auths, uid)
	delete(ps.conns, uid)

	return nil
}
