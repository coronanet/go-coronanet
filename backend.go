// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/coronanet/go-coronanet/tornet"
	"github.com/cretz/bine/control"
	"github.com/cretz/bine/tor"
	"github.com/ipsn/go-libtor"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Backend represents the social network node that can connect to other nodes in
// the network and exchange information.
type Backend struct {
	database *leveldb.DB // Database to avoid custom file formats for storage
	network  *tor.Tor    // Proxy through the Tor network, nil when offline

	overlay *tornet.Node // Overlay network running the Corona protocol
	pairing *pairer      // Currently active pairing session (nil if none)

	lock sync.RWMutex
}

// NewBackend creates a new social network node.
func NewBackend(datadir string) (*Backend, error) {
	// Create the database for accessing locally stored data
	db, err := leveldb.OpenFile(filepath.Join(datadir, "ldb"), &opt.Options{})
	if err != nil {
		return nil, err
	}
	// Create the Tor background process for accessing remote data
	net, err := tor.Start(nil, &tor.StartConf{
		ProcessCreator:         libtor.Creator,
		UseEmbeddedControlConn: true,
		DataDir:                filepath.Join(datadir, "tor"),
		DebugWriter:            os.Stderr,
		NoHush:                 true,
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	// All good, return the idle backend
	return &Backend{
		database: db,
		network:  net,
	}, nil
}

// Enable opens up the network proxy into the Tor network and starts building
// out the P2P overlay network on top. The method is async.
func (b *Backend) Enable() error {
	// Ensure the node is not yet enabled
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.overlay != nil {
		return nil
	}
	// Ensure we have a crypto profile available
	prof, err := b.Profile()
	if err != nil {
		return err
	}
	// Enable the network asynchronously to avoid blocking if something funky
	// happens (or we don't have internet) and assemble the overlay on top
	if err := b.network.EnableNetwork(context.Background(), false); err != nil {
		return nil
	}
	overlay := tornet.New(tornet.NewTorGateway(b.network), prof.Key, prof.Ring, nil)
	if err := overlay.Start(); err != nil {
		// Something went wrong, tear down the Tor circuits. TODO(karalabe): upstream as `b.network.DisableNetwork`?
		if err := b.network.Control.SetConf(control.KeyVals("DisableNetwork", "1")...); err != nil {
			panic(err)
		}
		return err
	}
	b.overlay = overlay
	return nil
}

// Disable tears down the P2P overlay network running on top of Tor, breaks all
// active connections and closes off he network proxy from Tor.
func (b *Backend) Disable() error {
	// Ensure the node is not yet disabled
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.overlay == nil {
		return nil
	}
	// Terminate the P2P overlay and disconnect from Tor
	b.overlay.Stop()
	b.overlay = nil

	if err := b.network.Control.SetConf(control.KeyVals("DisableNetwork", "1")...); err != nil {
		panic(err)
	}
	return nil
}

// Status returns whether the backend has networking enabled, whether that works
// or not; and the total download and upload traffic incurred since starting it.
func (b *Backend) Status() (bool, bool, uint64, uint64, error) {
	// Retrieve whether the network is enabled or not
	res, err := b.network.Control.GetConf("DisableNetwork")
	if err != nil {
		return false, false, 0, 0, err
	}
	enabled := res[0].Val == "0"

	// Retrieve some status metrics from Tor itself
	res, err = b.network.Control.GetInfo("status/circuit-established", "traffic/read", "traffic/written", "network-liveness")
	if err != nil {
		return enabled, false, 0, 0, err
	}
	connected := res[0].Val == "1" // TODO(karalabe): this doesn't seem to detect going offline, help?

	ingress, err := strconv.ParseUint(res[1].Val, 0, 64)
	if err != nil {
		return enabled, connected, 0, 0, err
	}
	egress, err := strconv.ParseUint(res[2].Val, 0, 64)
	if err != nil {
		return enabled, connected, ingress, 0, err
	}
	return enabled, connected, ingress, egress, nil
}
