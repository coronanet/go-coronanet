// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package pairing

import (
	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/tornet"
)

const (
	// Protocol is the unique identifier of the pairing protocol.
	Protocol = "pairing"
)

// Envelope is an envelope containing all possible messages received through
// the `pairing` wire protocol.
type Envelope struct {
	Disconnect *protocols.Disconnect
	Identity   *Identity
}

// Identity sends the user's `social` protocol P2P identity.
type Identity struct {
	Identity tornet.PublicIdentity // Identity to authenticate with
	Address  tornet.PublicAddress  // Address to contact through
}
