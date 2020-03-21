// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package system defines the messages for the base system protocol.
package system

// Handshake represents the initial protocol version negotiation.
type Handshake struct {
	Protocol string // Protocol expected on this connection
	Versions []uint // Protocol version numbers supported
}

// Disconnect represents a notification that the connection is torn down.
type Disconnect struct {
	Reason string // Textual disconnect reason, meant for developers
}

// Heartbeat is a notification that the client is still alive.
type Heartbeat struct{}
