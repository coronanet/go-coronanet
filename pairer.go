// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/coronanet/go-coronanet/protocol/pairing"
	"github.com/coronanet/go-coronanet/protocol/system"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// pairer runs the pairing algorithm with a remote peer, hopefully at the end
// of it resulting in a remote identity.
type pairer struct {
	self tornet.RemoteKeyRing // Real identity to send to the remote peer
	peer tornet.RemoteKeyRing // Real identity to receive from the remote peer

	peerset *tornet.PeerSet // Peer set handling remote connections
	server  *tornet.Server  // Ephemeral pairing server through the Tor network

	singleton chan struct{} // Guard channel to only ever allow one run
	finished  chan struct{} // Notification channel when pairing finishes
	failure   error         // Failure that occurred during the pairing exchange

	enc *gob.Encoder // Gob encoder for sending messages
	dec *gob.Decoder // Gob decoder for reading messages
}

// newPairingServer creates a temporary tornet server running a pairing protocol
// and attempts to exchange the real identities of two peers. Internally it creates
// an ephemeral identity to be advertised on a unique, temporary side channel.
//
// The method returns a secret identity to authenticate with in both directions
// and a public address to connect to. It is super unorthodox to reuse the same
// encryption key in both directions, but it avoids having to send 2 identities
// to the joiner (which would make QR codes quite unwieldy).
func newPairingServer(gateway tornet.Gateway, self tornet.RemoteKeyRing) (*pairer, tornet.SecretIdentity, tornet.PublicAddress, error) {
	// Pairing will be done on an ephemeral channel, create a temporary identity
	// for it, reusing the same for both directions.
	identity, err := tornet.GenerateIdentity()
	if err != nil {
		return nil, nil, nil, err
	}
	address, err := tornet.GenerateAddress()
	if err != nil {
		return nil, nil, nil, err
	}
	// Create a temporary tornet server to accept the pairing connection on
	p := &pairer{
		self:      self,
		singleton: make(chan struct{}, 1),
		finished:  make(chan struct{}),
	}
	p.peerset = tornet.NewPeerSet(tornet.PeerSetConfig{
		Trusted: []tornet.PublicIdentity{identity.Public()},
		Handler: p.handle,
	})
	p.server, err = tornet.NewServer(tornet.ServerConfig{
		Gateway:  gateway,
		Address:  address,
		Identity: identity,
		PeerSet:  p.peerset,
	})
	if err != nil {
		p.peerset.Close()
		return nil, nil, nil, err
	}
	return p, identity, address.Public(), nil
}

// newPairingClient creates a temporary tornet client running a pairing protocol
// and attempts to exchange the real identities of two peers. Internally it uses
// a pre-distributed ephemeral identity to connect to a temporary side channel.
func newPairingClient(gateway tornet.Gateway, self tornet.RemoteKeyRing, identity tornet.SecretIdentity, address tornet.PublicAddress) (*pairer, error) {
	p := &pairer{
		self:      self,
		singleton: make(chan struct{}, 1),
		finished:  make(chan struct{}),
	}
	p.peerset = tornet.NewPeerSet(tornet.PeerSetConfig{
		Trusted: []tornet.PublicIdentity{identity.Public()},
		Handler: p.handle,
	})
	if err := tornet.DialServer(context.TODO(), tornet.DialConfig{
		Gateway:  gateway,
		Address:  address,
		Server:   identity.Public(),
		Identity: identity,
		PeerSet:  p.peerset,
	}); err != nil {
		p.peerset.Close()
		return nil, err
	}
	return p, nil
}

// wait blocks until the pairing is done or the context is cancelled.
func (p *pairer) wait(ctx context.Context) (tornet.RemoteKeyRing, error) {
	defer p.peerset.Close()
	if p.server != nil {
		defer p.server.Close()
	}
	select {
	case <-ctx.Done():
		return tornet.RemoteKeyRing{}, errors.New("context cancelled")
	case <-p.finished:
		if p.failure != nil {
			return tornet.RemoteKeyRing{}, p.failure
		}
		return p.peer, nil
	}
}

// handle is the handler for the pairing protocol.
//
// See: https://github.com/coronanet/go-coronanet/blob/master/spec/wire.md
func (p *pairer) handle(uid tornet.IdentityFingerprint, conn net.Conn) {
	// Create a logger to track what's going on
	logger := log.New("pairer", uid)
	logger.Info("Pairer connected")

	// If the pairing already in progress, reject additional peers
	select {
	case p.singleton <- struct{}{}:
		// Singleton lock received, everyone's happy
	case <-p.finished:
		log.Error("Pairing session already finished")
		return
	default:
		log.Error("Pairing session already in progress")
		return
	}
	// No matter what happens, mark the pairer finished after this point
	defer close(p.finished)

	// Create the gob encoder and decoder
	p.enc = gob.NewEncoder(conn)
	p.dec = gob.NewDecoder(conn)

	// All protocols start with a system handshake, send ours, read theirs
	errc := make(chan error, 2)
	go func() {
		errc <- p.enc.Encode(&system.Handshake{Protocol: pairing.Protocol, Versions: []uint{1}})
	}()
	handshake := new(system.Handshake)
	go func() {
		errc <- p.dec.Decode(handshake)
	}()

	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				p.failure = err
				return
			}
		case <-timeout.C:
			p.failure = errors.New("handshake timed out")
			return
		}
	}
	// Find the common protocol, abort otherwise
	if handshake.Protocol != pairing.Protocol {
		p.failure = fmt.Errorf("unexpected pairing protocol: %s", handshake.Protocol)
		return
	}
	var version uint
	for _, v := range handshake.Versions {
		if v == 1 { // Bit forced with only 1 supported version, should be better later
			version = 1
			break
		}
	}
	if version == 0 {
		p.failure = fmt.Errorf("no common protocol version: %v vs %v", []uint{1}, handshake.Protocol)
		return
	}
	// Yay, handshake completed, run requested version
	switch version {
	case 1:
		p.failure = p.handleV1()
	default:
		panic(fmt.Sprintf("unhandled pairing protocol version: %d", version))
	}
}

// handleV1 is the handler for the v1 pairing protocol.
//
// See: https://github.com/coronanet/go-coronanet/blob/master/spec/wire.md
func (p *pairer) handleV1() error {
	// Send out identity, read theirs
	errc := make(chan error, 2)
	go func() {
		errc <- p.enc.Encode(&pairing.Identity{Blob: append(p.self.Identity, p.self.Address...)})
	}()
	identity := new(pairing.Identity)
	go func() {
		errc <- p.dec.Decode(identity)
	}()

	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return err
			}
		case <-timeout.C:
			return errors.New("handshake timed out")
		}
	}
	// Decode the received identity and return
	if len(identity.Blob) != len(p.self.Identity)+len(p.self.Address) {
		return fmt.Errorf("invalid response length: %d", len(identity.Blob))
	}
	p.peer = tornet.RemoteKeyRing{
		Identity: identity.Blob[:len(p.self.Identity)],
		Address:  identity.Blob[len(p.self.Identity):],
	}
	return nil
}
