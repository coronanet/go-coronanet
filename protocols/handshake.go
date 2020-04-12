// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package protocols

import (
	"encoding/gob"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/ethereum/go-ethereum/log"
)

// HandlerConfig specifies how a generic handshake should run and what methods
// should be given control when it succeeds.
type HandlerConfig struct {
	Protocol string           // Protocol to negotiate through the handshake
	Handlers map[uint]Handler // Handlers to run for different versions
}

// Handler is a callback to give control after a successful handshake.
type Handler func(uid tornet.IdentityFingerprint, conn net.Conn, enc *gob.Encoder, dec *gob.Decoder, logger log.Logger)

// MakeHandler creates a protocol handler based on the specified handshake and
// callback configurations. It's mostly sugar coating to avoid having to redo
// the same boilerplate in every protocol separately.
func MakeHandler(config HandlerConfig) tornet.ConnHandler {
	return func(uid tornet.IdentityFingerprint, conn net.Conn, logger log.Logger) {
		// Create a logger to track what's going on
		logger = logger.New("proto", config.Protocol, "peer", uid)
		logger.Info("Remote peer connected")

		// Create the gob encoder and decoder
		enc := gob.NewEncoder(conn)
		dec := gob.NewDecoder(conn)

		// Run the protocol handshake and catch any errors. Since we're not yet in
		// the separate reader/writer phase, we can't send over errors. Just nuke
		// the connection.
		versions := make([]uint, 0, len(config.Handlers))
		for v := range config.Handlers {
			versions = append(versions, v)
		}
		ver, err := handleHandshake(config.Protocol, versions, enc, dec)
		if err != nil {
			logger.Warn("Protocol handshake failed", "err", err)
			return
		}
		// Common protocol version negotiated, start up the actual message handler
		logger.Debug("Negotiated protocol version", "version", ver)
		config.Handlers[ver](uid, conn, enc, dec, logger)
	}
}

// handleHandshake runs a generic protocol negotiation and returns the common version
// number agreed upon.
func handleHandshake(protocol string, versions []uint, enc *gob.Encoder, dec *gob.Decoder) (uint, error) {
	// All protocols start with a system handshake, send ours, read theirs
	errc := make(chan error, 2)
	go func() {
		errc <- enc.Encode(&Handshake{Protocol: protocol, Versions: versions})
	}()
	handshake := new(Handshake)
	go func() {
		errc <- dec.Decode(handshake)
	}()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return 0, err
			}
		case <-timeout.C:
			return 0, errors.New("handshake timed out")
		}
	}
	// Find the common protocol, abort otherwise
	if handshake.Protocol != protocol {
		return 0, fmt.Errorf("unexpected protocol: %s", handshake.Protocol)
	}
	have := make(map[uint]struct{})
	for _, v := range versions {
		have[v] = struct{}{}
	}
	var version uint
	for _, v := range handshake.Versions {
		if _, ok := have[v]; ok && version < v {
			version = v
		}
	}
	if version == 0 {
		return 0, fmt.Errorf("no common protocol version: remote %v vs local %v", handshake.Versions, versions)
	}
	return version, nil
}
