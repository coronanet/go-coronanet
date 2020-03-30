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
	"github.com/ethereum/go-ethereum/log"
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

	scheduleKeyring    chan tornet.SecretKeyRing // Scheduler channel when the keyring is updated
	scheduleNetwork    chan struct{}             // Scheduler channel when network access changes
	scheduleTeardown   chan chan struct{}        // Scheduler channel when the system is terminating
	scheduleTerminated chan struct{}             // Termination channel to unblock any schedules

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
	// Create an idle backend; if there's already a user profile, assemble the overlay
	backend := &Backend{
		database:           db,
		network:            net,
		scheduleKeyring:    make(chan tornet.SecretKeyRing),
		scheduleTeardown:   make(chan chan struct{}),
		scheduleTerminated: make(chan struct{}),
	}
	go backend.scheduler()

	if prof, err := backend.Profile(); err == nil {
		if err := backend.initOverlay(*prof.KeyRing); err != nil {
			net.Close()
			db.Close()
			return nil, err
		}
	}
	return backend, nil
}

// initOverlay initializes the cryptographic tornet overlay on top of the existing
// Tor gateway according to the keyring in profile.
//
// Note, this method assumes the write lock is held.
func (b *Backend) initOverlay(keyring tornet.SecretKeyRing) error {
	log.Info("Creating overlay node", "addresses", len(keyring.Addresses), "contacts", len(keyring.Trusted))
	if b.overlay != nil {
		panic("overlay double initialized")
	}
	overlay, err := tornet.NewNode(tornet.NodeConfig{
		Gateway:     tornet.NewTorGateway(b.network),
		KeyRing:     keyring,
		RingHandler: b.updateKeyring,
		ConnHandler: b.handleContact,
	})
	if err != nil {
		return err
	}
	b.overlay = overlay

	// Since the overlay was constructed, ping the scheduler to start up
	select {
	case b.scheduleKeyring <- keyring:
	case <-b.scheduleTerminated:
	}
	return nil
}

// nukeOverlay tears down the entire overlay network built on top of Tor.
//
// Note, this method assumes the write lock is held.
func (b *Backend) nukeOverlay() error {
	log.Info("Deleting overlay node")
	if b.overlay == nil {
		return nil
	}
	err := b.overlay.Close()
	b.overlay = nil

	// Since the overlay was deleted, ping the scheduler to spin down
	select {
	case b.scheduleKeyring <- tornet.SecretKeyRing{}:
	case <-b.scheduleTerminated:
	}
	return err
}

// Close tears down the backend. It's irreversible, it cannot be used afterwards.
func (b *Backend) Close() error {
	// Stop initiating any outbound connections
	closer := make(chan struct{})
	b.scheduleTeardown <- closer
	<-closer

	// Stop accepting any inbound connections and drop everyone
	b.nukeOverlay()

	// Disable and tear down the Tor gateway
	b.network.Close()
	b.network = nil

	// Close the database and return
	b.database.Close()
	b.database = nil

	return nil
}

// Enable opens up the network proxy into the Tor network and starts building
// out the P2P overlay network on top. The method is async.
func (b *Backend) Enable() error {
	log.Info("Enabling gateway networking")
	return b.network.EnableNetwork(context.Background(), false)
}

// Disable tears down the P2P overlay network running on top of Tor, breaks all
// active connections and closes off he network proxy from Tor.
func (b *Backend) Disable() error {
	log.Info("Disabling gateway networking")
	return b.network.Control.SetConf(control.KeyVals("DisableNetwork", "1")...)
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
