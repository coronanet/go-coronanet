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
)

// pairer runs the pairing algorithm with a remote peer, hopefully at the end
// of it resulting in a remote identity.
type pairer struct {
	self *tornet.PublicIdentity // Real identity to send to the remote peer
	peer *tornet.PublicIdentity // Real identity to receive from the remote peer

	channel  *tornet.Node  // Ephemeral pairing channel through the Tor network
	failure  error         // Failure that occurred during the pairing exchange
	finished chan struct{} // Notification channel when pairing finishes

	enc *gob.Encoder // Gob encoder for sending messages
	dec *gob.Decoder // Gob decoder for reading messages
}

// newPairingServer creates a temporary tornet running a pairing protocol and
// attempts to exchange the real identities of two peers. Internally it creates
// an ephemeral identity to advertise on a unique, temporary side channel.
//
// The method returns a single public identity that acts as the discovery onion
// address as well as the authentication TLS certificate for **both** the client
// and server (essentially a shared secret). It is super unorthodox to reuse the
// same certificate in both directions, but it avoids having to send 2 identities
// to the joiner (which would make QR codes quite unwieldy).
func newPairingServer(gateway tornet.Gateway, id *tornet.PublicIdentity) (*pairer, *tornet.SecretIdentity, error) {
	// Pairing will be done on an ephemeral channel, create a temporary identity
	// for it, reusing the same for both directions.
	secret, err := tornet.GenerateIdentity()
	if err != nil {
		return nil, nil, err
	}
	// Establish a new temporary tornet to accept the pairing connection on
	p := &pairer{
		self:     id,
		finished: make(chan struct{}, 1),
	}
	p.channel = tornet.New(gateway, secret, map[string]*tornet.PublicIdentity{"": secret.Public()}, p.handle)
	if err := p.channel.StartOnlyServe(); err != nil {
		return nil, nil, err
	}
	return p, secret, nil
}

// newPairingClient creates a temporary tornet running a pairing protocol and
// attempts to exchange the real identities of two peers. Internally it uses
// a pre-distributed ephemeral identity to connect to a temporary side channel.
func newPairingClient(gateway tornet.Gateway, id *tornet.PublicIdentity, secret *tornet.SecretIdentity) (*pairer, error) {
	p := &pairer{
		self:     id,
		finished: make(chan struct{}, 1),
	}
	p.channel = tornet.New(gateway, secret, map[string]*tornet.PublicIdentity{"": secret.Public()}, p.handle)
	if err := p.channel.StartOnlyDial(); err != nil {
		return nil, err
	}
	return p, nil
}

// wait blocks until the pairing is done or the context is cancelled.
func (p *pairer) wait(ctx context.Context) (*tornet.PublicIdentity, error) {
	select {
	case <-ctx.Done():
		return nil, errors.New("context cancelled")
	case <-p.finished:
		if p.failure != nil {
			return nil, p.failure
		}
		return p.peer, nil
	}
}

// handle is the handler for the pairing protocol.
//
// See: https://github.com/coronanet/go-coronanet/blob/master/spec/wire.md
func (p *pairer) handle(id string, conn net.Conn) (err error) {
	// Create the gob encoder and decoder
	p.enc = gob.NewEncoder(conn)
	p.dec = gob.NewDecoder(conn)

	// Make sure the connection is torn down but send any errors
	defer conn.Close()
	defer func() {
		if err != nil {
			p.failure = err
		}
		close(p.finished)
	}()
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
				return err
			}
		case <-timeout.C:
			return errors.New("handshake timed out")
		}
	}
	// Find the common protocol, abort otherwise
	if handshake.Protocol != pairing.Protocol {
		return fmt.Errorf("unexpected pairing protocol: %s", handshake.Protocol)
	}
	var version uint
	for _, v := range handshake.Versions {
		if v == 1 { // Bit forced with only 1 supported version, should be better later
			version = 1
			break
		}
	}
	if version == 0 {
		return fmt.Errorf("no common protocol version: %v vs %v", []uint{1}, handshake.Protocol)
	}
	// Yay, handshake completed, run requested version
	switch version {
	case 1:
		return p.handleV1()
	default:
		panic(fmt.Sprintf("unhandled pairing protocol version: %d", version))
	}
}

// handleV1 is the handler for the v1 pairing protocol.
//
// See: https://github.com/coronanet/go-coronanet/blob/master/spec/wire.md
func (p *pairer) handleV1() error {
	blob, err := p.self.MarshalJSON()
	if err != nil {
		panic(err)
	}
	// Send out identity, read theirs
	errc := make(chan error, 2)
	go func() {
		errc <- p.enc.Encode(&pairing.Identity{Blob: blob})
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
	id := new(tornet.PublicIdentity)
	if err := id.UnmarshalJSON(identity.Blob); err != nil {
		return err
	}
	p.peer = id
	return nil
}
