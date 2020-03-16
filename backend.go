// coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"fmt"
	"sync"

	"github.com/cretz/bine/tor"
	"github.com/ipsn/go-libtor"
)

// backend represents the social network node that can connect to other nodes in
// the network and exchange information.
type backend struct {
	proxy *tor.Tor // Proxy through the Tor network, nil when offline

	lock sync.RWMutex
}

// newBackend creates a new social network node.
func newBackend() (*backend, error) {
	return &backend{}, nil
}

// Enable creates the network proxy into the Tor network.
func (b *backend) Enable() error {
	// Ensure the node is not yet enabled
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.proxy != nil {
		return nil
	}
	// Create the Tor background process and let it bootstrap itself async.
	proxy, err := tor.Start(nil, &tor.StartConf{ProcessCreator: libtor.Creator, UseEmbeddedControlConn: true})
	if err != nil {
		return err
	}
	b.proxy = proxy
	return nil
}

// Disable stops and tears down the network proxy into the Tor network.
func (b *backend) Disable() error {
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

// Status returns whether the backend has networking enabled and the total down-
// and upload traffic incurred since starting it.
func (b *backend) Status() (bool, uint64, uint64, error) {
	// If the node is offline, return all zeroes
	b.lock.RLock()
	defer b.lock.RUnlock()

	if b.proxy == nil {
		return false, 0, 0, nil
	}
	// Tor proxy online, retrieve the current stats
	res, err := b.proxy.Control.GetInfo("traffic/read", "traffic/written")
	if err != nil {
		return true, 0, 0, err
	}
	fmt.Println(res[0].Val, res[1].Val)
	return true, 0, 0, nil
}
