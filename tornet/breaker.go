// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package tornet

import (
	"net"
	"time"
)

// breaker is a net.Conn wrapper that automatically disconnect if no data exchange
// happens for a pre-configured amount of time.
type breaker struct {
	net.Conn // Pass everything non-interesting through

	timeout time.Duration // Duration to reset to on traffic
	breaker *time.Timer   // Timer that will break the connection
}

// newBreaker creates a net.Conn wrapper that breaks after a pre-configured time.
func newBreaker(conn net.Conn, timeout time.Duration) net.Conn {
	return &breaker{
		Conn:    conn,
		timeout: timeout,
		breaker: time.AfterFunc(timeout, func() { conn.Close() }),
	}
}

// Read implements net.Conn, resetting the idle timer within the connection.
func (b *breaker) Read(buf []byte) (int, error) {
	b.breaker.Reset(b.timeout)
	return b.Conn.Read(buf)
}

// Write implements net.Conn, resetting the idle timer within the connection.
func (b *breaker) Write(buf []byte) (int, error) {
	b.breaker.Reset(b.timeout)
	return b.Conn.Write(buf)
}
