// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/cretz/bine/tor"
	"github.com/ipsn/go-libtor"
)

// Backend represents the social network node that can connect to other nodes in
// the network and exchange information.
type Backend struct {
	datadir string   // Data directory to use for Tor and the database
	proxy   *tor.Tor // Proxy through the Tor network, nil when offline

	lock sync.RWMutex
}

// NewBackend creates a new social network node.
func NewBackend(datadir string) (*Backend, error) {
	return &Backend{datadir: datadir}, nil
}

// Enable creates the network proxy into the Tor network.
func (b *Backend) Enable() error {
	// Ensure the node is not yet enabled
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.proxy != nil {
		return nil
	}
	// Create the Tor background process and let it bootstrap itself async.
	proxy, err := tor.Start(nil, &tor.StartConf{
		ProcessCreator:         libtor.Creator,
		UseEmbeddedControlConn: true,
		EnableNetwork:          true,
		DataDir:                b.datadir,
		DebugWriter:            os.Stderr,
		NoHush:                 true,
	})
	if err != nil {
		return err
	}
	b.proxy = proxy
	return nil
}

// Disable stops and tears down the network proxy into the Tor network.
func (b *Backend) Disable() error {
	// Ensure the node is not yet disabled
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.proxy == nil {
		return nil
	}
	// Proxy still functional, terminate it and return
	if err := b.proxy.Close(); err != nil {
		return err
	}
	b.proxy = nil
	return nil
}

// Status returns whether the backend has networking enabled, whether that works
// or not; and the total download and upload traffic incurred since starting it.
func (b *Backend) Status() (bool, bool, uint64, uint64, error) {
	// If the node is offline, return all zeroes
	b.lock.RLock()
	defer b.lock.RUnlock()

	if b.proxy == nil {
		return false, false, 0, 0, nil
	}
	// Tor proxy online, retrieve the current stats
	res, err := b.proxy.Control.GetInfo("network-liveness", "traffic/read", "traffic/written")
	if err != nil {
		return true, false, 0, 0, err
	}
	var connected bool
	switch res[0].Val {
	case "up":
		connected = true
	case "down":
		connected = false
	default:
		return true, false, 0, 0, fmt.Errorf("unknown network liveness: %v", res[0].Val)
	}
	ingress, err := strconv.ParseUint(res[1].Val, 0, 64)
	if err != nil {
		return true, connected, 0, 0, err
	}
	egress, err := strconv.ParseUint(res[2].Val, 0, 64)
	if err != nil {
		return true, connected, ingress, 0, err
	}
	return true, connected, ingress, egress, nil
}
