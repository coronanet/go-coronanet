// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"errors"

	"github.com/coronanet/go-coronanet/tornet"
)

var (
	// ErrNetworkDisabled is returned if an operation is requested which requires
	// network access but it is not enabled.
	ErrNetworkDisabled = errors.New("network disabled")

	// ErrAlreadyPairing is returned if a pairing session is attempted to be
	// initiated, but one is already in progress.
	ErrAlreadyPairing = errors.New("already pairing")

	// ErrNotPairing is returned if a pairing session is attempted to be joined,
	// but none is in progress.
	ErrNotPairing = errors.New("not pairing")
)

// InitPairing initiates a new pairing session over Tor.
func (b *Backend) InitPairing() (*tornet.SecretIdentity, error) {
	// Ensure there is no pairing session ongoing
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.overlay == nil {
		return nil, ErrNetworkDisabled
	}
	if b.pairing != nil {
		return nil, ErrAlreadyPairing
	}
	// No pairing session running, create a new one
	prof, err := b.Profile()
	if err != nil {
		panic(err) // Overlay cannot exist without a profile
	}
	pairer, secret, err := newPairingServer(tornet.NewTorGateway(b.network), prof.Key.Public())
	if err != nil {
		return nil, err
	}
	b.pairing = pairer
	return secret, nil
}

// WaitPairing blocks until an already initiated pairing session is joined.
func (b *Backend) WaitPairing() (*tornet.PublicIdentity, error) {
	// Ensure there is a pairing session ongoing
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.pairing == nil {
		return nil, ErrNotPairing
	}
	// Pairing session in progress, wait for it and tear it down
	id, err := b.pairing.wait(context.TODO())
	b.pairing = nil
	return id, err
}

// JoinPairing joins a remotely initiated pairing session.
func (b *Backend) JoinPairing(secret *tornet.SecretIdentity) (*tornet.PublicIdentity, error) {
	// Ensure we are in a pairable state
	b.lock.RLock()
	defer b.lock.RUnlock()

	if b.overlay == nil {
		return nil, ErrNetworkDisabled
	}
	prof, err := b.Profile()
	if err != nil {
		panic(err) // Overlay cannot exist without a profile
	}
	// Join the remote pairing session and return the results
	pairer, err := newPairingClient(tornet.NewTorGateway(b.network), prof.Key.Public(), secret)
	if err != nil {
		return nil, err
	}
	return pairer.wait(context.TODO())
}
