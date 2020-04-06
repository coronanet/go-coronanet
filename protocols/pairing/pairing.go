// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package pairing implements the pairing protocol.
package pairing

import (
	"context"
	"encoding/gob"
	"errors"
	"net"
	"time"

	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// Pairing runs the pairing algorithm with a remote peer, hopefully at the end
// of it resulting in a remote identity.
type Pairing struct {
	self tornet.RemoteKeyRing // Real identity to send to the remote peer
	peer tornet.RemoteKeyRing // Real identity to receive from the remote peer

	peerset *tornet.PeerSet // Peer set handling remote connections
	server  *tornet.Server  // Ephemeral pairing server through the Tor network

	singleton chan struct{} // Guard channel to only ever allow one run
	finished  chan struct{} // Notification channel when pairing finishes
	failure   error         // Failure that occurred during the pairing exchange
}

// NewServer creates a temporary tornet server running a pairing protocol and
// attempts to exchange the real identities of two peers. Internally it creates
// an ephemeral identity to be advertised on a unique, temporary side channel.
//
// The method returns a secret identity to authenticate with in both directions
// and a public address to connect to. It is super unorthodox to reuse the same
// encryption key in both directions, but it avoids having to send 2 identities
// to the joiner (which would make QR codes quite unwieldy).
func NewServer(gateway tornet.Gateway, self tornet.RemoteKeyRing) (*Pairing, tornet.SecretIdentity, tornet.PublicAddress, error) {
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
	p := &Pairing{
		self:      self,
		singleton: make(chan struct{}, 1),
		finished:  make(chan struct{}),
	}
	p.peerset = tornet.NewPeerSet(tornet.PeerSetConfig{
		Trusted: []tornet.PublicIdentity{identity.Public()},
		Handler: protocols.MakeHandler(protocols.HandlerConfig{
			Protocol: Protocol,
			Handlers: map[uint]protocols.Handler{
				1: p.handleV1,
			},
		}),
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

// NewClient creates a temporary tornet client running a pairing protocol and
// attempts to exchange the real identities of two peers. Internally it uses
// a pre-distributed ephemeral identity to connect to a temporary side channel.
func NewClient(gateway tornet.Gateway, self tornet.RemoteKeyRing, identity tornet.SecretIdentity, address tornet.PublicAddress) (*Pairing, error) {
	p := &Pairing{
		self:      self,
		singleton: make(chan struct{}, 1),
		finished:  make(chan struct{}),
	}
	p.peerset = tornet.NewPeerSet(tornet.PeerSetConfig{
		Trusted: []tornet.PublicIdentity{identity.Public()},
		Handler: protocols.MakeHandler(protocols.HandlerConfig{
			Protocol: Protocol,
			Handlers: map[uint]protocols.Handler{
				1: p.handleV1,
			},
		}),
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

// Wait blocks until the pairing is done or the context is cancelled.
func (p *Pairing) Wait(ctx context.Context) (tornet.RemoteKeyRing, error) {
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

// handleV1 is the handler for the v1 pairing protocol.
func (p *Pairing) handleV1(logger log.Logger, uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder) {
	// If the pairing already in progress, reject additional peers
	select {
	case p.singleton <- struct{}{}:
		// Singleton lock received, everyone's happy
	case <-p.finished:
		logger.Error("Pairing session already finished")
		return
	default:
		logger.Error("Pairing session already in progress")
		return
	}
	// No matter what happens, mark the pairer finished after this point
	defer close(p.finished)

	// Send out identity, read theirs
	errc := make(chan error, 2)
	go func() {
		errc <- enc.Encode(&Envelope{
			Identity: &Identity{
				Identity: p.self.Identity,
				Address:  p.self.Address,
			},
		})
	}()
	message := new(Envelope)
	go func() {
		errc <- dec.Decode(message)
	}()

	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				logger.Warn("Identity exchange failed", "err", err)
				return
			}
		case <-timeout.C:
			logger.Warn("Identity exchange timed out")
			return
		}
	}
	// Decode the received identity and return
	if message.Identity == nil {
		logger.Warn("Missing identity exchange")
		return
	}
	p.peer = tornet.RemoteKeyRing{
		Identity: message.Identity.Identity,
		Address:  message.Identity.Address,
	}
	logger.Info("Paired with new identity", "identity", p.peer.Identity.Fingerprint(), "address", p.peer.Address.Fingerprint())
}
