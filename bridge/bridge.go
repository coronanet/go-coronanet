// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

// Package bridge is exposes the Corona Network to gomobile.
package bridge

import (
	"os"

	"github.com/coronanet/go-coronanet"
	"github.com/coronanet/go-coronanet/rest"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ipsn/go-ghostbridge"
)

// Push all the logs out to Android's logcat.
func init() {
	log.Root().SetHandler(log.LvlFilterHandler(5, log.StreamHandler(os.Stderr, log.TerminalFormat(false))))
}

// Bridge is a tiny struct (re)definition so gomobile will export all the built
// in methods of the underlying ghostbridge.Bridge struct.
type Bridge struct {
	*ghostbridge.Bridge
	backend *coronanet.Backend
}

// NewBridge creates an instance of the ghost bridge, typed such as gomobile to
// generate a Bridge constructor out of it.
func NewBridge(datadir string) (*Bridge, error) {
	backend, err := coronanet.NewBackend(datadir)
	if err != nil {
		return nil, err
	}
	bridge, err := ghostbridge.New(rest.New("", backend))
	if err != nil {
		return nil, err
	}
	return &Bridge{
		Bridge:  bridge,
		backend: backend,
	}, nil
}

// GatewayStatus is a simplified status report from the gateway to be used by
// native notifications on mobile platforms.
type GatewayStatus struct {
	Enabled   bool
	Connected bool
	Ingress   int64
	Egress    int64
}

// GatewayStatus is a pass-through method to allow directly calling Backend.Status
// via  the mobile library. This is useful for showing native notifications without
// screwing with HTTP and certificates.
func (b *Bridge) GatewayStatus() (*GatewayStatus, error) {
	enabled, connected, ingress, egress, err := b.backend.GatewayStatus()
	if err != nil {
		return nil, err
	}
	return &GatewayStatus{
		Enabled:   enabled,
		Connected: connected,
		Ingress:   int64(ingress),
		Egress:    int64(egress),
	}, nil
}

// EnableGateway is a pass-through method to allow directly calling Backend.Enable
// via  the mobile library. This is useful for showing native notifications without
// screwing with HTTP and certificates.
func (b *Bridge) EnableGateway() error {
	return b.backend.EnableGateway()
}

// DisableGateway is a pass-through method to allow directly calling Backend.Disable
// via  the mobile library. This is useful for showing native notifications without
// screwing with HTTP and certificates.
func (b *Bridge) DisableGateway() error {
	return b.backend.DisableGateway()
}
