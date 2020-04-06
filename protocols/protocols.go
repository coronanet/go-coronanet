// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package protocols defines the messages common for all protocols.
package protocols

// Handshake represents the initial protocol version negotiation.
type Handshake struct {
	Protocol string // Protocol expected on this connection
	Versions []uint // Protocol version numbers supported
}

// Disconnect represents a notification that the connection is torn down.
type Disconnect struct {
	Reason string // Textual disconnect reason, meant for developers
}
