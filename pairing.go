// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"errors"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
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
func (b *Backend) InitPairing() (tornet.SecretIdentity, tornet.PublicAddress, error) {
	log.Info("Initiating pairing session")

	// Ensure there is no pairing session ongoing
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.pairing != nil {
		return nil, nil, ErrAlreadyPairing
	}
	// No pairing session running, create a new one
	prof, err := b.Profile()
	if err != nil {
		panic(err) // Overlay cannot exist without a profile
	}
	keyring := tornet.RemoteKeyRing{
		Identity: prof.KeyRing.Identity.Public(),
		Address:  prof.KeyRing.Addresses[len(prof.KeyRing.Addresses)-1].Public(),
	}
	pairer, secret, address, err := newPairingServer(tornet.NewTorGateway(b.network), keyring)
	if err != nil {
		return nil, nil, err
	}
	b.pairing = pairer
	return secret, address, nil
}

// WaitPairing blocks until an already initiated pairing session is joined.
func (b *Backend) WaitPairing() (tornet.RemoteKeyRing, error) {
	log.Info("Waiting for pairing session")

	// Ensure there is a pairing session ongoing
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.pairing == nil {
		return tornet.RemoteKeyRing{}, ErrNotPairing
	}
	// Pairing session in progress, wait for it and tear it down
	keyring, err := b.pairing.wait(context.TODO())
	b.pairing = nil
	return keyring, err
}

// JoinPairing joins a remotely initiated pairing session.
func (b *Backend) JoinPairing(secret tornet.SecretIdentity, address tornet.PublicAddress) (tornet.RemoteKeyRing, error) {
	log.Info("Joining pairing session", "address", address.Fingerprint(), "identity", secret.Fingerprint())

	// Ensure we are in a pairable state
	prof, err := b.Profile()
	if err != nil {
		panic(err) // Overlay cannot exist without a profile
	}
	// Join the remote pairing session and return the results
	keyring := tornet.RemoteKeyRing{
		Identity: prof.KeyRing.Identity.Public(),
		Address:  prof.KeyRing.Addresses[len(prof.KeyRing.Addresses)-1].Public(),
	}
	pairer, err := newPairingClient(tornet.NewTorGateway(b.network), keyring, secret, address)
	if err != nil {
		return tornet.RemoteKeyRing{}, err
	}
	return pairer.wait(context.TODO())
}
