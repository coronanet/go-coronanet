// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package corona defines the messages for the main corona protocol.
package corona

// Protocol is the unique identifier of the corona protocol.
const Protocol = "corona"

// GetProfile requests the remote user's profile summary.
type GetProfile struct{}

// Profile sends the current user's profile summary.
type Profile struct {
	Name   string   // Free form name the user is advertising (might be fake)
	Avatar [32]byte // SHA3 hash of the user's avatar (avoid download if known)
}

// GetAvatar requests the remote user's profile picture.
type GetAvatar struct{}

// Avatar sends the current user's profile picture.
type Avatar struct {
	Image []byte // Binary image content, mime not restricted for now
}
