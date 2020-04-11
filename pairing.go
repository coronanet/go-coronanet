// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"errors"
	"time"

	"github.com/coronanet/go-coronanet/protocols/pairing"
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

	// Ensure there's a profile to pair and a network to go through
	profile, err := b.Profile()
	if err != nil {
		return nil, nil, err
	}
	online, connected, _, _, err := b.GatewayStatus()
	if err != nil {
		return nil, nil, err
	}
	if !online {
		return nil, nil, ErrNetworkDisabled
	}
	if online && !connected {
		// This is problematic. We're supposedly online, but there's no circuit
		// yet. The happy case is that the gateway was just enabled, so let's
		// wait a bit and hope.
		//
		// This might not be too useful during live operation, but it's something
		// needed for tests since those spin too fast for Tor to set everything up
		// and things just fail because of it.
		for i := 0; i < 60 && !connected; i++ {
			log.Warn("Waiting for circuits to build", "attempt", i)

			time.Sleep(time.Second)
			_, connected, _, _, err = b.GatewayStatus()
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if !connected {
		return nil, nil, errors.New("no circuits available")
	}
	// Ensure there is no pairing session ongoing
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.pairing != nil {
		return nil, nil, ErrAlreadyPairing
	}
	// No pairing session running, create a new one
	keyring := tornet.RemoteKeyRing{
		Identity: profile.KeyRing.Identity.Public(),
		Address:  profile.KeyRing.Addresses[len(profile.KeyRing.Addresses)-1].Public(),
	}
	pairer, secret, address, err := pairing.NewServer(tornet.NewTorGateway(b.network), keyring)
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
	keyring, err := b.pairing.Wait(context.TODO())
	b.pairing = nil
	return keyring, err
}

// JoinPairing joins a remotely initiated pairing session.
func (b *Backend) JoinPairing(secret tornet.SecretIdentity, address tornet.PublicAddress) (tornet.RemoteKeyRing, error) {
	log.Info("Joining pairing session", "address", address.Fingerprint(), "identity", secret.Fingerprint())

	// Ensure there's a profile to pair and a network to go through
	profile, err := b.Profile()
	if err != nil {
		return tornet.RemoteKeyRing{}, err
	}
	online, connected, _, _, err := b.GatewayStatus()
	if err != nil {
		return tornet.RemoteKeyRing{}, err
	}
	if !online {
		return tornet.RemoteKeyRing{}, ErrNetworkDisabled
	}
	if online && !connected {
		// This is problematic. We're supposedly online, but there's no circuit
		// yet. The happy case is that the gateway was just enabled, so let's
		// wait a bit and hope.
		//
		// This might not be too useful during live operation, but it's something
		// needed for tests since those spin too fast for Tor to set everything up
		// and things just fail because of it.
		for i := 0; i < 60 && !connected; i++ {
			log.Warn("Waiting for circuits to build", "attempt", i)

			time.Sleep(time.Second)
			_, connected, _, _, err = b.GatewayStatus()
			if err != nil {
				return tornet.RemoteKeyRing{}, err
			}
		}
	}
	if !connected {
		return tornet.RemoteKeyRing{}, errors.New("no circuits available")
	}
	// Join the remote pairing session and return the results
	keyring := tornet.RemoteKeyRing{
		Identity: profile.KeyRing.Identity.Public(),
		Address:  profile.KeyRing.Addresses[len(profile.KeyRing.Addresses)-1].Public(),
	}
	pairer, err := pairing.NewClient(tornet.NewTorGateway(b.network), keyring, secret, address)
	if err != nil {
		return tornet.RemoteKeyRing{}, err
	}
	return pairer.Wait(context.TODO())
}

// TODO(karalabe): AbortPairing, otherwise we end up in a weird place
