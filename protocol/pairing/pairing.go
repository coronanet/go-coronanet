// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package pairing defines the messages for side pairing.
package pairing

const (
	// Protocol is the unique identifier of the pairing protocol.
	Protocol = "pairing"
)

// Identity sends the user's Corona protocol P2P identity.
type Identity struct {
	Blob []byte // Encoded tornet public identity, internal format
}
