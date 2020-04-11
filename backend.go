// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package coronanet

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/coronanet/go-coronanet/protocols"
	"github.com/coronanet/go-coronanet/protocols/corona"
	"github.com/coronanet/go-coronanet/protocols/events"
	"github.com/coronanet/go-coronanet/protocols/pairing"
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

	// Social protocol and related fields
	overlay *tornet.Node     // Overlay network running the Corona protocol
	dialer  *scheduler       // Dial scheduler to periodically connect to peers
	pairing *pairing.Pairing // Currently active pairing session (nil if none)

	peerset map[tornet.IdentityFingerprint]*gob.Encoder // Current active connections for updates

	// Event protocol and related fields
	hosted  map[tornet.IdentityFingerprint]*events.Server         // Locally hosted and maintained events
	checkin map[tornet.IdentityFingerprint]*events.CheckinSession // Active checkin session per hosted event
	joined  map[tornet.IdentityFingerprint]*events.Client         // Remotely joined and watched events

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
		database: db,
		network:  net,
		peerset:  make(map[tornet.IdentityFingerprint]*gob.Encoder),
	}
	backend.dialer = newScheduler(backend)

	if prof, err := backend.Profile(); err == nil {
		if err := backend.initOverlay(*prof.KeyRing); err != nil {
			net.Close()
			db.Close()
			return nil, err
		}
	}
	return backend, nil
}

// initOverlay initializes the application layer networking protocols that will
// the within the backend.
//
// Note, this method assumes the write lock is held.
func (b *Backend) initOverlay(keyring tornet.SecretKeyRing) error {
	// Create the social network node for the contact list
	log.Info("Creating social node", "addresses", len(keyring.Addresses), "contacts", len(keyring.Trusted))
	if b.overlay != nil {
		panic("overlay double initialized")
	}
	overlay, err := tornet.NewNode(tornet.NodeConfig{
		Gateway:     tornet.NewTorGateway(b.network),
		KeyRing:     keyring,
		RingHandler: b.updateKeyring,
		ConnHandler: protocols.MakeHandler(protocols.HandlerConfig{
			Protocol: corona.Protocol,
			Handlers: map[uint]protocols.Handler{
				1: b.handleContactV1,
			},
		}),
		ConnTimeout: connectionIdleTimeout,
	})
	if err != nil {
		return err
	}
	b.overlay = overlay

	// Create the event servers and clients for meetup tracking
	if err := b.initEvents(); err != nil {
		b.overlay.Close()
		b.overlay = nil
		return err
	}
	return nil
}

// nukeOverlay tears down the entire application overlay network.
//
// Note, this method assumes the write lock is held.
func (b *Backend) nukeOverlay() error {
	// Tear down the event servers and clients
	b.nukeEvents()

	// Tear down the social networking
	log.Info("Deleting social node")
	if b.overlay == nil {
		return nil
	}
	err := b.overlay.Close()
	b.overlay = nil

	// Since the overlay was deleted, ping the scheduler to spin down
	b.dialer.suspend()
	return err
}

// Close tears down the backend. It's irreversible, it cannot be used afterwards.
func (b *Backend) Close() error {
	// Stop initiating and accepting outbound connections, drop everyone
	b.dialer.close()
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
	if err := b.network.EnableNetwork(context.Background(), false); err != nil {
		return err
	}
	// Networking enabled, resume all scheduled dials
	prof, err := b.Profile()
	if err != nil {
		return nil // No profile is fine
	}
	b.dialer.reinit(*prof.KeyRing)

	b.lock.RLock()
	for _, client := range b.joined {
		client.Resume()
	}
	b.lock.RUnlock()
	return nil
}

// Disable tears down the P2P overlay network running on top of Tor, breaks all
// active connections and closes off he network proxy from Tor.
func (b *Backend) Disable() error {
	log.Info("Disabling gateway networking")
	if err := b.network.Control.SetConf(control.KeyVals("DisableNetwork", "1")...); err != nil {
		return err
	}
	// Networking disabled, suspend all scheduled dials as pointless
	b.dialer.suspend()

	b.lock.RLock()
	for _, client := range b.joined {
		client.Suspend()
	}
	b.lock.RUnlock()

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
