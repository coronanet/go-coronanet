// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

// SecretKeyRing is the ultimate collection of cryptographic identities and
// relations for a local user. These are the keys to the castle.
//
// This struct is mostly a helper for grouping things in a meaningful way for
// the rest of the system and implementing a few marshalling methods for sharing
// over various transports.
//
// The complexity of multiple addresses and mappings are to enable rotating Tor
// onions gradually, introducing new ones and shifting out old ones after every
// contact has been moved over.
type SecretKeyRing struct {
	Identity  SecretIdentity  `json:"identity"`  // Secret stable identity. This is you.
	Addresses []SecretAddress `json:"addresses"` // Secret semi-stable addresses. These are where you are.

	Trusted  map[IdentityFingerprint]RemoteKeyRing                   `json:"trusted"`  // Remote identities trusted for communication
	Accesses map[AddressFingerprint]map[IdentityFingerprint]struct{} `json:"accesses"` // Addresses that specific remote identities can dial
}

// RemoteKeyRing is a small collection of cryptographic keys maintained about a
// remote user.
type RemoteKeyRing struct {
	Identity PublicIdentity `json:"identity"` // Remote stable identity. This is your contact.
	Address  PublicAddress  `json:"address"`  // Remote semi-stable address. This is where your contact is.
}

// GenerateKeyRing generates a new cryptographic identity and initial contact
// address for tornet.
func GenerateKeyRing() (SecretKeyRing, error) {
	identity, err := GenerateIdentity()
	if err != nil {
		return SecretKeyRing{}, nil
	}
	address, err := GenerateAddress()
	if err != nil {
		return SecretKeyRing{}, nil
	}
	return SecretKeyRing{
		Identity:  identity,
		Addresses: []SecretAddress{address},
		Trusted:   make(map[IdentityFingerprint]RemoteKeyRing),
		Accesses: map[AddressFingerprint]map[IdentityFingerprint]struct{}{
			address.Fingerprint(): make(map[IdentityFingerprint]struct{}),
		},
	}, nil
}
